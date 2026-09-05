#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
trap diagnostics ERR
if kubectl -n envy-baseline get virtualservice envy-service-b >/dev/null 2>&1; then
  echo 'The control plane already owns service-b routing. Run the manual spike in another ENVY_CLUSTER_NAME, with unused ENVY_PREVIEW_PORT and ENVY_API_PORT.' >&2
  exit 1
fi
python3 "$ENVY_ROOT/deploy/local/verify-spike.py" before
kubectl apply -f "$ENVY_ROOT/deploy/kubernetes/spike.yaml"
kubectl -n envy-spike rollout status deployment/service-b --timeout=180s
python3 "$ENVY_ROOT/deploy/local/verify-spike.py" active
kubectl -n envy-baseline delete virtualservice envy-spike-ingress --ignore-not-found
python3 "$ENVY_ROOT/deploy/local/verify-spike.py" unpublished
kubectl -n envy-baseline delete virtualservice envy-spike-service-b --ignore-not-found
kubectl delete namespace envy-spike --ignore-not-found --wait=true --timeout=120s
python3 "$ENVY_ROOT/deploy/local/verify-spike.py" after
echo 'Routing spike passed: baseline v1, composition v2, context normalization, identity preservation, and cleanup.'
