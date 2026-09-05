#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
kind delete cluster --name "$ENVY_CLUSTER_NAME"
rm -f "$KUBECONFIG"
