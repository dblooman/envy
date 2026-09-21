#!/usr/bin/env bash
# Synthetic composite preview acceptance, confined to a newly created kind cluster.
set -euo pipefail
export ENVY_CLUSTER_NAME=${ENVY_CLUSTER_NAME:-envy-composite-e2e}
export ENVY_PREVIEW_PORT=${ENVY_PREVIEW_PORT:-29080}
export ENVY_API_PORT=${ENVY_API_PORT:-29081}
source "$(dirname "$0")/common.sh"
[[ "$ENVY_CLUSTER_NAME" == envy-composite-* ]] || { echo 'Requires a disposable envy-composite-* cluster' >&2; exit 1; }
[[ -z "$(docker ps -aq --filter "label=io.x-k8s.kind.cluster=$ENVY_CLUSTER_NAME")" ]] || { echo 'Cluster already exists; choose another name' >&2; exit 1; }
rm -f "$ENVY_STATE_DIR/composite-acceptance.log"
cleanup() {
  code=$?
  if [[ $code != 0 ]]; then
    diagnostics
    kubectl -n envy-system logs deployment/envy-server --all-containers=true > "$ENVY_STATE_DIR/server.log" 2>&1 || true
  fi
  if [[ ${ENVY_E2E_KEEP_CLUSTER:-0} != 1 ]]; then kind delete cluster --name "$ENVY_CLUSTER_NAME"; fi
  exit "$code"
}
trap cleanup EXIT
bash "$ENVY_ROOT/deploy/local/bootstrap.sh"
bash "$ENVY_ROOT/deploy/local/build-demo.sh"
bash "$ENVY_ROOT/deploy/local/baseline.sh"
bash "$ENVY_ROOT/deploy/local/control-plane.sh"
docker build -f "$ENVY_ROOT/deploy/local/composite-fixture/Dockerfile" -t envy/composite-helper:v1 "$ENVY_ROOT"
kind load docker-image --name "$ENVY_CLUSTER_NAME" envy/composite-helper:v1
# Resolve the image already loaded in this node. Supporting containers are
# pinned to an immutable OCI manifest, with no registry or private credentials.
node="$ENVY_CLUSTER_NAME-control-plane"
helper_tag=docker.io/envy/composite-helper:v1
helper_digest=$(docker exec "$node" ctr -n k8s.io images ls | awk -v tag="$helper_tag" '$1 == tag {print $3}')
[[ "$helper_digest" == sha256:* ]] || { echo 'Cannot pin the composite helper image' >&2; exit 1; }
export ENVY_COMPOSITE_HELPER_IMAGE="docker.io/envy/composite-helper@$helper_digest"
docker exec "$node" ctr -n k8s.io images tag --force "$helper_tag" "$ENVY_COMPOSITE_HELPER_IMAGE"
kubectl -n envy-system get configmap envy-server-policy -o json > "$ENVY_STATE_DIR/server-policy.json"
python3 "$ENVY_ROOT/deploy/local/composite-fixture/setup.py" "$ENVY_ROOT" "$ENVY_STATE_DIR" "$ENVY_COMPOSITE_HELPER_IMAGE"
kubectl apply -f "$ENVY_STATE_DIR/composite-fixture.json"
kubectl apply -f "$ENVY_STATE_DIR/composite-permissions.json"
kubectl -n envy-system create configmap envy-server-policy --from-file=config.json="$ENVY_STATE_DIR/composite-server-config.json" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n envy-system rollout restart deployment/envy-server
kubectl -n envy-system rollout status deployment/envy-server --timeout=180s
kubectl -n envy-baseline rollout status deployment/service-b --timeout=180s
for service in shared-dependency gateway service-a service-b; do
  kubectl -n envy-composite-baseline rollout status "deployment/$service" --timeout=180s
done
export ENVY_API_URL="http://127.0.0.1:$ENVY_API_PORT"
unset ENVY_API_TOKEN_FILE ENVY_API_TOKEN
export ENVY_COMPOSITE_TEST=1
cd "$ENVY_ROOT"
go test -tags=e2e ./tests/e2e -run '^TestCompositePreviewLifecycle$' -v -count=1 -timeout=12m | tee "$ENVY_STATE_DIR/composite-acceptance.log"
