#!/usr/bin/env bash
set -euo pipefail
provider=${1:?usage: e2e.sh istio|cilium|linkerd}
export ENVY_CLUSTER_NAME=${ENVY_CLUSTER_NAME:-envy-test-$provider}
export ENVY_PREVIEW_PORT=${ENVY_PREVIEW_PORT:-19080}
export ENVY_API_PORT=${ENVY_API_PORT:-19081}
source "$(dirname "$0")/../local/common.sh"
# A caller may resume a fixture it just created; normal CI always bootstraps fresh.
if [[ ${ENVY_TEST_RESUME:-0} != 1 ]]; then bash "$ENVY_ROOT/deploy/testing/bootstrap.sh" "$provider"; fi
cleanup(){ code=$?; diagnostics; if [[ ${ENVY_E2E_KEEP_CLUSTER:-0} != 1 ]]; then kind delete cluster --name "$ENVY_CLUSTER_NAME"; fi; exit "$code"; }
trap cleanup EXIT
bash "$ENVY_ROOT/deploy/testing/build-images.sh"
python3 "$ENVY_ROOT/deploy/testing/setup.py" "$provider"
if [[ "$provider" == linkerd ]]; then python3 "$ENVY_ROOT/deploy/testing/check-linkerd-conformance.py"; fi
export ENVY_API_URL="http://127.0.0.1:$ENVY_API_PORT" ENVY_API_TOKEN_FILE="$ENVY_STATE_DIR/api-token"
export ENVY_MCP_BINARY="$ENVY_ROOT/.envy/bin/envy-mcp" ENVY_CLI_BINARY="$ENVY_ROOT/.envy/bin/delivery"
export ENVY_TEST_SERVER_DEPLOYMENT=envy-envy ENVY_TEST_SERVER_SELECTOR=app.kubernetes.io/name=envy
if [[ ${ENVY_TEST_HTTPS:-1} == 1 ]]; then export ENVY_TEST_CA_FILE="$ENVY_STATE_DIR/preview.crt"; fi
cd "$ENVY_ROOT"
go test -tags=e2e ./tests/e2e -count=1 -v -timeout=25m -run 'TestCompositionLifecycle|TestMultipleOverridesAcrossRESTMCPAndCLI|TestCLIImageUpdatePreservesComposition|TestMCPCompositionThroughStdio|TestExpiryAcrossRestart|TestMeshCapacity|TestMixedBaggageInsideMesh' | tee "$ENVY_STATE_DIR/acceptance.log"
kubectl get pods -A -o json > "$ENVY_STATE_DIR/accepted-pods.json"
