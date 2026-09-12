#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"

HELM="$ENVY_ROOT/.envy/bin/helm"
namespace="envy-helm-smoke"
release="smoke"

test -x "$HELM" || { echo "run: GOBIN=$ENVY_ROOT/.envy/bin go install helm.sh/helm/v3/cmd/helm@v3.19.0" >&2; exit 1; }
cleanup() {
  "$HELM" uninstall "$release" --namespace "$namespace" >/dev/null 2>&1 || true
  kubectl delete namespace "$namespace" --wait=true >/dev/null 2>&1 || true
}
trap cleanup EXIT

kubectl create namespace "$namespace" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n "$namespace" create secret generic envy-database \
  --from-literal=url="postgres://envy:$(cat "$ENVY_STATE_DIR/postgres-password")@envy-postgres.envy-system.svc.cluster.local:5432/envy?sslmode=disable" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
docker build -f "$ENVY_ROOT/Dockerfile.server" -t envy/server:dev "$ENVY_ROOT" >/dev/null
kind load docker-image --name "$ENVY_CLUSTER_NAME" envy/server:dev >/dev/null
"$HELM" upgrade --install "$release" "$ENVY_ROOT/deploy/helm/envy" --namespace "$namespace" \
  --set installationID=helm-smoke --set image.repository=envy/server --set image.tag=dev --set image.pullPolicy=Never \
  --set externalDatabase.secretName=envy-database --set externalDatabase.secretKey=url \
  --set auth.mode=none --set auth.proxySecret.name= \
  --set runtime.previewBaseURL=https://envy.smoke.test \
  --set runtime.ingressURL=https://istio-ingressgateway.istio-system.svc.cluster.local \
  --set runtime.baselineHost=baseline.smoke.test --wait --timeout=120s >/dev/null
kubectl -n "$namespace" rollout status deployment/smoke-envy --timeout=90s >/dev/null
echo "Helm smoke installation passed"
