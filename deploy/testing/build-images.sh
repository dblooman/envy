#!/usr/bin/env bash
# Compile on the host to avoid contention with mesh controllers inside Docker's VM.
set -euo pipefail
source "$(dirname "$0")/../local/common.sh"
arch=$(docker info --format '{{.Architecture}}')
case "$arch" in aarch64|arm64) arch=arm64;; x86_64|amd64) arch=amd64;; *) echo "unsupported Docker architecture $arch" >&2; exit 1;; esac
context="$ENVY_STATE_DIR/image-context"
mkdir -p "$context"
for service in gateway service-a service-b; do
 versions='v1 v2'; [[ "$service" != service-b ]] || versions='v1 v2 v3'
 for version in $versions; do
  GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/dblooman/envy/demo/internal/server.Version=$version" -o "$context/demo" "$ENVY_ROOT/demo/$service"
  printf 'FROM scratch\nCOPY demo /demo\nUSER 65532:65532\nENTRYPOINT ["/demo"]\n' > "$context/Dockerfile"
  docker build -t "envy/$service:$version" "$context"
 done
done
GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$context/envy-server" "$ENVY_ROOT/cmd/server"
pnpm --dir "$ENVY_ROOT/web" install --frozen-lockfile
pnpm --dir "$ENVY_ROOT/web" build
mkdir -p "$context/web"
cp -R "$ENVY_ROOT/web/dist/." "$context/web/"
ca_bundle=/etc/ssl/certs/ca-certificates.crt
[[ -f "$ca_bundle" ]] || ca_bundle=/etc/ssl/cert.pem
cp "$ca_bundle" "$context/ca-certificates.crt"
printf 'FROM scratch\nCOPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt\nCOPY envy-server /envy-server\nCOPY web /web\nUSER 65532:65532\nENTRYPOINT ["/envy-server"]\n' > "$context/Dockerfile"
docker build -t envy/server:mesh-test "$context"
kind load docker-image --name "$ENVY_CLUSTER_NAME" envy/server:mesh-test envy/gateway:v1 envy/gateway:v2 envy/service-a:v1 envy/service-a:v2 envy/service-b:v1 envy/service-b:v2 envy/service-b:v3
make -C "$ENVY_ROOT" build
