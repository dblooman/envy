#!/usr/bin/env bash
set -euo pipefail
export ENVY_CLUSTER_NAME=${ENVY_CLUSTER_NAME:-envy-derived-e2e}
export ENVY_PREVIEW_PORT=${ENVY_PREVIEW_PORT:-28080}
export ENVY_API_PORT=${ENVY_API_PORT:-28081}
source "$(dirname "$0")/common.sh"
[[ "$ENVY_CLUSTER_NAME" == envy-derived-* ]] || { echo 'Requires a disposable envy-derived-* cluster' >&2; exit 1; }
[[ -z "$(docker ps -aq --filter "label=io.x-k8s.kind.cluster=$ENVY_CLUSTER_NAME")" ]] || { echo 'Cluster already exists; choose another name' >&2; exit 1; }
cleanup(){
 code=$?
 if [[ $code != 0 ]]; then diagnostics; fi
 if [[ ${ENVY_E2E_KEEP_CLUSTER:-0} != 1 ]]; then kind delete cluster --name "$ENVY_CLUSTER_NAME"; fi
 exit "$code"
}
trap cleanup EXIT
bash "$ENVY_ROOT/deploy/local/bootstrap.sh"
bash "$ENVY_ROOT/deploy/local/build-demo.sh"
bash "$ENVY_ROOT/deploy/local/baseline.sh"
bash "$ENVY_ROOT/deploy/local/control-plane.sh"
bash "$ENVY_ROOT/deploy/local/derived-argo-setup.sh"
export ENVY_API_URL="http://127.0.0.1:$ENVY_API_PORT"
unset ENVY_API_TOKEN_FILE ENVY_API_TOKEN
export ENVY_CLI_BINARY="$ENVY_ROOT/.envy/bin/envy"
export ENVY_DERIVED_ARGO_TEST=1
cd "$ENVY_ROOT"
go test -tags=e2e ./tests/e2e -run TestDerivedPreviewsWithArgo -v -count=1 -timeout=15m | tee "$ENVY_STATE_DIR/derived-acceptance.log"
