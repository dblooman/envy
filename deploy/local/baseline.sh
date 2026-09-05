#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
trap diagnostics ERR
kubectl apply -f "$ENVY_ROOT/deploy/kubernetes/baseline.yaml"
for service in service-b service-a gateway; do
  kubectl -n envy-baseline rollout status "deployment/$service" --timeout=180s
done
