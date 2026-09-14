#!/usr/bin/env bash
set -euo pipefail
ENVY_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
export ENVY_CLUSTER_NAME=${ENVY_CLUSTER_NAME:-envy-dev}
export ENVY_STATE_DIR=${ENVY_STATE_DIR:-$ENVY_ROOT/.envy/$ENVY_CLUSTER_NAME}
export ENVY_PREVIEW_PORT=${ENVY_PREVIEW_PORT:-8080}
export ENVY_API_PORT=${ENVY_API_PORT:-8081}
export KUBECONFIG="$ENVY_STATE_DIR/kubeconfig"
ENVY_TOOLS="$ENVY_ROOT/.envy/tools"
export PATH="$ENVY_TOOLS:$PATH"
fixture_version() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))[sys.argv[2]])' "$ENVY_ROOT/deploy/testing/versions.json" "$1"; }
KIND_VERSION=$(fixture_version kind)
ISTIO_VERSION=$(fixture_version istio)
NODE_IMAGE=$(fixture_version node_image)
if [[ ! "$ENVY_CLUSTER_NAME" =~ ^envy-[a-z0-9-]+$ ]]; then
  echo 'Refusing to operate on a cluster whose name does not start with envy-.' >&2; exit 1
fi
mkdir -p "$ENVY_STATE_DIR" "$ENVY_TOOLS"
chmod 700 "$ENVY_STATE_DIR"

diagnostics() {
  kubectl get pods -A -o wide > "$ENVY_STATE_DIR/pods.txt" 2>&1 || true
  kubectl get events -A --sort-by=.lastTimestamp > "$ENVY_STATE_DIR/events.txt" 2>&1 || true
  kubectl get httproutes,referencegrants -A -o yaml > "$ENVY_STATE_DIR/routes.yaml" 2>&1 || kubectl get virtualservices -A -o yaml > "$ENVY_STATE_DIR/routes.yaml" 2>&1 || true
  echo "Diagnostics: $ENVY_STATE_DIR" >&2
}
