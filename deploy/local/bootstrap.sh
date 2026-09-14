#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
trap diagnostics ERR
command -v docker >/dev/null
command -v kubectl >/dev/null
command -v python3 >/dev/null
docker info >/dev/null
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m); [[ "$arch" != aarch64 ]] || arch=arm64; [[ "$arch" != x86_64 ]] || arch=amd64
case "$os-$arch" in darwin-arm64|darwin-amd64|linux-amd64|linux-arm64) ;; *) echo "Unsupported platform $os-$arch" >&2; exit 1;; esac
check_hash() { python3 - "$1" "$2" <<'PY'
import hashlib,sys
actual=hashlib.sha256(open(sys.argv[1],'rb').read()).hexdigest()
if actual != sys.argv[2]: raise SystemExit('Checksum mismatch: '+sys.argv[1])
PY
}
if [[ ! -x "$ENVY_TOOLS/kind" ]]; then
  expected=$(awk -v name="kind-$os-$arch" '$2==name {print $1}' "$ENVY_ROOT/deploy/local/checksums.txt")
  curl -fSL --retry 3 --connect-timeout 10 --max-time 180 "https://github.com/kubernetes-sigs/kind/releases/download/v$KIND_VERSION/kind-$os-$arch" -o "$ENVY_TOOLS/kind.download"
  check_hash "$ENVY_TOOLS/kind.download" "$expected"
  mv "$ENVY_TOOLS/kind.download" "$ENVY_TOOLS/kind"; chmod +x "$ENVY_TOOLS/kind"
fi
if [[ ! -x "$ENVY_TOOLS/istioctl" ]]; then
  istio_os=$os; [[ "$os" != darwin ]] || istio_os=osx
  archive="istio-$ISTIO_VERSION-$istio_os-$arch.tar.gz"
  expected=$(awk -v name="$archive" '$2==name {print $1}' "$ENVY_ROOT/deploy/local/checksums.txt")
  curl -fSL --retry 3 --connect-timeout 10 --max-time 300 "https://github.com/istio/istio/releases/download/$ISTIO_VERSION/$archive" -o "$ENVY_TOOLS/$archive"
  check_hash "$ENVY_TOOLS/$archive" "$expected"
  tar -xzf "$ENVY_TOOLS/$archive" -C "$ENVY_TOOLS" "istio-$ISTIO_VERSION/bin/istioctl"
  mv "$ENVY_TOOLS/istio-$ISTIO_VERSION/bin/istioctl" "$ENVY_TOOLS/istioctl"
fi
if ! kind get clusters | awk -v name="$ENVY_CLUSTER_NAME" '$0==name {found=1} END {exit !found}'; then
  cat > "$ENVY_STATE_DIR/kind.yaml" <<YAML
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  apiServerAddress: "127.0.0.1"
nodes:
- role: control-plane
  image: "$NODE_IMAGE"
  extraPortMappings:
  - containerPort: 30080
    hostPort: $ENVY_PREVIEW_PORT
    listenAddress: "127.0.0.1"
    protocol: TCP
  - containerPort: 30081
    hostPort: $ENVY_API_PORT
    listenAddress: "127.0.0.1"
    protocol: TCP
YAML
  kind create cluster --name "$ENVY_CLUSTER_NAME" --config "$ENVY_STATE_DIR/kind.yaml" --kubeconfig "$KUBECONFIG" --wait 180s
else
  kind export kubeconfig --name "$ENVY_CLUSTER_NAME" --kubeconfig "$KUBECONFIG"
fi
chmod 600 "$KUBECONFIG"
image_pin() { python3 -c 'import json,sys; print(sys.argv[2]+"@"+json.load(open(sys.argv[1]))["images"][sys.argv[2]])' "$ENVY_ROOT/deploy/testing/versions.json" "$1"; }
istioctl install --kubeconfig "$KUBECONFIG" -y -f "$ENVY_ROOT/deploy/kubernetes/istio.yaml" \
 --set tag="$ISTIO_VERSION" \
 --set values.pilot.image="$(image_pin docker.io/istio/pilot:$ISTIO_VERSION)" \
 --set values.global.proxy.image="$(image_pin docker.io/istio/proxyv2:$ISTIO_VERSION)" --readiness-timeout 300s
kubectl -n istio-system patch svc istio-ingressgateway --type=strategic -p '{"spec":{"type":"NodePort","ports":[{"port":80,"nodePort":30080}]}}'
printf 'kind %s\nnode %s\nistio %s\n' "$KIND_VERSION" "$NODE_IMAGE" "$ISTIO_VERSION" > "$ENVY_STATE_DIR/versions.txt"
kubectl -n istio-system get pods -o jsonpath='{range .items[*]}{range .status.containerStatuses[*]}{.image}{" "}{.imageID}{"\n"}{end}{end}' > "$ENVY_STATE_DIR/istio-images.txt"
echo "Cluster $ENVY_CLUSTER_NAME ready; kubeconfig: $KUBECONFIG"
