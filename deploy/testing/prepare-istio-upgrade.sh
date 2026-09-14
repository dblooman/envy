#!/usr/bin/env bash
# Build the pinned previous server/chart to test an actual populated Istio upgrade.
set -euo pipefail
source "$(dirname "$0")/../local/common.sh"
revision=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["istio_upgrade_from"])' "$ENVY_ROOT/deploy/testing/versions.json")
if ! git -C "$ENVY_ROOT" cat-file -e "$revision^{commit}"; then git -C "$ENVY_ROOT" fetch --depth=1 origin "$revision"; fi
previous="$ENVY_STATE_DIR/upgrade-source"
mkdir -p "$previous"
git -C "$ENVY_ROOT" archive "$revision" | tar -x -C "$previous"
arch=$(docker info --format '{{.Architecture}}')
case "$arch" in aarch64|arm64) arch=arm64;; x86_64|amd64) arch=amd64;; *) exit 1;; esac
context="$ENVY_STATE_DIR/image-context"
(cd "$previous" && GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$context/envy-server" ./cmd/server)
docker build -t envy/server:upgrade-from "$context"
kind load docker-image --name "$ENVY_CLUSTER_NAME" envy/server:upgrade-from
