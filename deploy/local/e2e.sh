#!/usr/bin/env bash
set -euo pipefail
export ENVY_CLUSTER_NAME=${ENVY_CLUSTER_NAME:-envy-e2e}
export ENVY_PREVIEW_PORT=${ENVY_PREVIEW_PORT:-18080}
export ENVY_API_PORT=${ENVY_API_PORT:-18081}
source "$(dirname "$0")/common.sh"
# A fresh cluster is required; never delete an existing cluster implicitly.
if [[ -x "$ENVY_TOOLS/kind" ]] && kind get clusters | awk -v name="$ENVY_CLUSTER_NAME" '$0==name {found=1} END {exit !found}'; then
  echo "Cluster $ENVY_CLUSTER_NAME already exists. Choose another ENVY_CLUSTER_NAME or run ENVY_CLUSTER_NAME=$ENVY_CLUSTER_NAME make dev-down." >&2
  exit 1
fi
cleanup(){
  code=$?
  if [[ "$code" != 0 ]]; then diagnostics; kubectl -n envy-system logs deployment/envy-server --all-containers=true > "$ENVY_STATE_DIR/server.log" 2>&1 || true; fi
  if [[ "${ENVY_E2E_KEEP_CLUSTER:-0}" != 1 ]]; then kind delete cluster --name "$ENVY_CLUSTER_NAME"; fi
  exit "$code"
}
trap cleanup EXIT
bash "$ENVY_ROOT/deploy/local/bootstrap.sh"
bash "$ENVY_ROOT/deploy/local/build-demo.sh"
bash "$ENVY_ROOT/deploy/local/baseline.sh"
bash "$ENVY_ROOT/deploy/local/control-plane.sh"
export ENVY_API_URL="http://127.0.0.1:$ENVY_API_PORT"
export ENVY_API_TOKEN_FILE="$ENVY_STATE_DIR/api-token"
export ENVY_MCP_BINARY="$ENVY_ROOT/.envy/bin/envy-mcp"
cd "$ENVY_ROOT"
go test -tags=e2e ./tests/e2e -count=1 -v -timeout=20m | tee "$ENVY_STATE_DIR/acceptance.log"
