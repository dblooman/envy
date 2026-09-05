#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
for service in gateway service-a service-b; do
  docker build -f "$ENVY_ROOT/Dockerfile.demo" --build-arg SERVICE="$service" --build-arg VERSION=v1 -t "envy/$service:v1" "$ENVY_ROOT"
done
docker build -f "$ENVY_ROOT/Dockerfile.demo" --build-arg SERVICE=service-b --build-arg VERSION=v2 -t envy/service-b:v2 "$ENVY_ROOT"
kind load docker-image --name "$ENVY_CLUSTER_NAME" envy/gateway:v1 envy/service-a:v1 envy/service-b:v1 envy/service-b:v2
docker image inspect --format '{{.RepoTags}} {{.Id}}' envy/gateway:v1 envy/service-a:v1 envy/service-b:v1 envy/service-b:v2 > "$ENVY_STATE_DIR/demo-images.txt"
