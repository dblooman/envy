#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"

HELM="$ENVY_ROOT/.envy/bin/helm"
namespace="envy-helm-smoke-$(date +%s)-$$"
release="$namespace"
credentials_dir=""

test -x "$HELM" || { echo "run: GOBIN=$ENVY_ROOT/.envy/bin go install helm.sh/helm/v3/cmd/helm@v3.19.0" >&2; exit 1; }
cleanup() {
  if [[ -n "$credentials_dir" ]]; then rm -rf "$credentials_dir"; fi
  "$HELM" uninstall "$release" --namespace "$namespace" >/dev/null 2>&1 || true
  kubectl delete namespace "$namespace" --wait=true >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Refuse to reuse any existing namespace. Cleanup is armed only after creation.
trap - EXIT
kubectl create namespace "$namespace" >/dev/null
trap cleanup EXIT
credentials_dir=$(mktemp -d "$ENVY_STATE_DIR/helm-auth.XXXXXX")
chmod 700 "$credentials_dir"
python3 - "$namespace" <<'PYSECRET' | kubectl apply -f - >/dev/null
import json, secrets, sys
ns = sys.argv[1]
password = secrets.token_hex(32)
print(json.dumps({"apiVersion":"v1","kind":"Secret","metadata":{"name":"envy-database","namespace":ns},"stringData":{"password":password,"url":f"postgres://envy:{password}@smoke-postgres.{ns}.svc.cluster.local:5432/envy?sslmode=disable"}}))
PYSECRET
python3 - "$namespace" "$credentials_dir/credentials.json" <<'PYAUTH' | kubectl apply -f - >/dev/null
import json, os, secrets, sys
ns, path = sys.argv[1:]
credentials = {key: secrets.token_hex(32) for key in ("proxy", "machine", "session")}
with open(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as output:
    json.dump(credentials, output)
print(json.dumps({"apiVersion":"v1","kind":"Secret","metadata":{"name":"envy-auth","namespace":ns},"stringData":{"secret":credentials["proxy"],"credentials.json":json.dumps([{"id":"acceptance-agent","display_name":"Acceptance agent","token":credentials["machine"]}])}}))
PYAUTH
# Provision this database independently of Helm; never use development state.
kubectl -n "$namespace" apply -f - <<'YAML' >/dev/null
apiVersion: v1
kind: Pod
metadata:
  name: smoke-postgres
  labels: {app: smoke-postgres}
spec:
  automountServiceAccountToken: false
  containers:
    - name: postgres
      image: postgres:18.6@sha256:4ef4dbc939d61acea57712655ddb4b4ab27419c913f94cca0cd57cb3ea3c2280
      env:
        - {name: POSTGRES_USER, value: envy}
        - {name: POSTGRES_DB, value: envy}
        - name: POSTGRES_PASSWORD
          valueFrom: {secretKeyRef: {name: envy-database, key: password}}
      readinessProbe:
        exec: {command: [pg_isready, -U, envy, -d, envy]}
      volumeMounts: [{name: data, mountPath: /var/lib/postgresql}]
  volumes: [{name: data, emptyDir: {}}]
---
apiVersion: v1
kind: Service
metadata: {name: smoke-postgres}
spec:
  selector: {app: smoke-postgres}
  ports: [{port: 5432, targetPort: 5432}]
YAML
kubectl -n "$namespace" wait --for=condition=Ready pod/smoke-postgres --timeout=120s >/dev/null
bash "$ENVY_ROOT/deploy/local/build-demo.sh" >/dev/null
docker build -f "$ENVY_ROOT/Dockerfile.server" -t envy/server:dev "$ENVY_ROOT" >/dev/null
kind load docker-image --name "$ENVY_CLUSTER_NAME" envy/server:dev >/dev/null
"$HELM" upgrade --install "$release" "$ENVY_ROOT/deploy/helm/envy" --namespace "$namespace" \
  --set installationID=helm-smoke --set image.repository=envy/server --set image.tag=dev --set image.pullPolicy=Never \
  --set externalDatabase.secretName=envy-database --set externalDatabase.secretKey=url \
  --set auth.mode=proxy --set auth.proxySecret.name=envy-auth \
  --set auth.machineCredentialsSecret.name=envy-auth \
  --set-string auth.trustedProxyCIDRs=127.0.0.0/8 --set auth.externalOrigin=https://envy.acceptance.test \
  --set runtime.previewBaseURL=https://envy.smoke.test \
  --set runtime.ingressURL=https://istio-ingressgateway.istio-system.svc.cluster.local \
  --set runtime.baselineHost=baseline.smoke.test --wait --timeout=120s >/dev/null
kubectl -n "$namespace" rollout status deployment/"$release-envy" --timeout=90s >/dev/null
export ENVY_INSTALLATION_TEST_NAMESPACE="$namespace" ENVY_INSTALLATION_TEST_CREDENTIALS="$credentials_dir/credentials.json"
go test "$ENVY_ROOT/tests/installation" -run TestHTTPSProxyInstallation -count=1 -v
go test "$ENVY_ROOT/tests/installation" -run TestIstioHTTPSInstallation -count=1 -v
count=$(kubectl -n "$namespace" exec smoke-postgres -- psql -U envy -d envy -Atc "SELECT count(*) FROM compositions WHERE phase <> 'destroyed'")
test "$count" = 0 || { echo "Acceptance left active compositions" >&2; exit 1; }
"$HELM" uninstall "$release" --namespace "$namespace" >/dev/null
kubectl -n "$namespace" wait --for=condition=Ready pod/smoke-postgres --timeout=30s >/dev/null
echo "Helm smoke installation passed"
