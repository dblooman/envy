#!/usr/bin/env bash
# Run only on the cluster Mac. No global context changes or cluster creation.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
export KUBECONFIG=${KUBECONFIG:-$HOME/.kube/config}
: "${ENVY_GHCR_CONFIG_FILE:?Provide a read-only GHCR Docker config file}"
command -v helm >/dev/null
command -v istioctl >/dev/null
kubectl --context=docker-desktop cluster-info >/dev/null
docker info >/dev/null
# Enforce the tested mesh line; do not silently replace another installation.
istioctl version --remote=false | head -1 | grep -q '1.31.0' || { echo 'Use istioctl 1.31.0' >&2; exit 1; }
python3 deploy/lan/prepare-secrets.py
kubectl --context=docker-desktop apply -f deploy/lan/infrastructure.yaml
kubectl --context=docker-desktop -n envy-system rollout status deployment/envy-postgres --timeout=180s
istioctl install --context=docker-desktop -y --set profile=default \
  --set components.ingressGateways[0].name=istio-ingressgateway \
  --set components.ingressGateways[0].enabled=true
kubectl --context=docker-desktop -n istio-system patch service istio-ingressgateway --type=strategic \
  -p '{"spec":{"type":"NodePort","ports":[{"port":80,"nodePort":30080}]}}'
# Use Kyverno's official GHCR images; Docker Desktop may truncate pulls from
# the chart's default reg.kyverno.io registry.
helm upgrade --install kyverno kyverno --repo https://kyverno.github.io/kyverno/ --version 3.8.2 \
  --kube-context docker-desktop --namespace kyverno --create-namespace \
  --set global.image.registry=ghcr.io --wait --timeout 180s
kubectl --context=docker-desktop apply -f deploy/lan/secret-policy.yaml
# Build a clean commit; never reuse a mutable dev tag in this installation.
test -z "$(git status --porcelain --untracked-files=no)" || { echo 'Commit tracked changes before building the installation image' >&2; exit 1; }
revision=$(git rev-parse HEAD)
docker build --provenance=false -f Dockerfile.server -t "envy/server:$revision" .
helm upgrade --install envy deploy/helm/envy --kube-context docker-desktop -n envy-system \
  -f deploy/lan/values.yaml --set "image.tag=$revision" --wait --timeout 180s
# ErrImageNeverPull means this Desktop configuration does not share Docker's image
# store with Kubernetes. Stop here; do not substitute kind or a different image.
kubectl --context=docker-desktop -n envy-system rollout status deployment/envy-envy --timeout=120s
python3 deploy/lan/render.py --builds deploy/lan/builds.json
kubectl --context=docker-desktop apply -f .envy/lan/baseline.json
for name in storefront pricing; do
  kubectl --context=docker-desktop -n envy-lan-baseline rollout status "deployment/$name" --timeout=180s
done
# Preflight is explicit after the borrowed baseline exists; reinstalling Envy
# itself does not create application workloads or catalog entries.
kubectl --context=docker-desktop -n envy-system delete job envy-envy-preflight --ignore-not-found=true
helm template envy deploy/helm/envy -f deploy/lan/values.yaml --set "image.tag=$revision" \
  --set preflight.enabled=true --show-only templates/preflight-job.yaml -n envy-system \
  | kubectl --context=docker-desktop -n envy-system apply -f -
kubectl --context=docker-desktop -n envy-system wait --for=condition=complete job/envy-envy-preflight --timeout=90s
kubectl --context=docker-desktop version -o json > .envy/lan/kubernetes-version.json
istioctl version --context=docker-desktop > .envy/lan/istio-version.txt
helm list --kube-context docker-desktop -A -o json > .envy/lan/releases.json
printf '%s\n' "$revision" > .envy/lan/envy-revision.txt
echo 'Infrastructure and baseline prepared. Register .envy/lan/catalog.json explicitly through the API, then run acceptance from the other laptop.'
