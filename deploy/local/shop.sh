#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
trap diagnostics ERR
for version in v1 v2; do
  docker build -f "$ENVY_ROOT/examples/shop/Dockerfile" --build-arg VERSION="$version" -t "envy/shop:$version" "$ENVY_ROOT"
done
kind load docker-image --name "$ENVY_CLUSTER_NAME" envy/shop:v1 envy/shop:v2
kubectl apply -f "$ENVY_ROOT/examples/shop/baseline.yaml"
for service in storefront pricing; do
  kubectl -n envy-shop rollout status deployment/"$service" --timeout=120s
done
docker image inspect --format '{{.RepoTags}} {{.Id}}' envy/shop:v1 envy/shop:v2 > "$ENVY_STATE_DIR/shop-images.txt"
echo 'Shop baseline deployed. Register examples/shop/application.json with envy catalog validate/apply.'
