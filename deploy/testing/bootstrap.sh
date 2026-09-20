#!/usr/bin/env bash
# Explicit disposable mesh fixture. Never changes an existing cluster.
set -euo pipefail
provider=${1:?usage: bootstrap.sh istio|cilium|linkerd}
case "$provider" in istio|cilium|linkerd) ;; *) echo 'choose istio, cilium or linkerd' >&2; exit 1;; esac
export ENVY_CLUSTER_NAME=${ENVY_CLUSTER_NAME:-envy-test-$provider}
export ENVY_PREVIEW_PORT=${ENVY_PREVIEW_PORT:-19080}
export ENVY_API_PORT=${ENVY_API_PORT:-19081}
source "$(dirname "$0")/../local/common.sh"
command -v kind >/dev/null; command -v helm >/dev/null
if docker ps -aq --filter "label=io.x-k8s.kind.cluster=$ENVY_CLUSTER_NAME" | read -r existing; then
 echo "Refusing existing cluster $ENVY_CLUSTER_NAME" >&2; exit 1
fi
version() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))[sys.argv[2]])' "$ENVY_ROOT/deploy/testing/versions.json" "$1"; }
digest() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["images"][sys.argv[2]])' "$ENVY_ROOT/deploy/testing/versions.json" "$1"; }
if [[ "$provider" == istio && ${ENVY_TEST_ENFORCE_POLICY:-0} != 1 ]]; then bash "$ENVY_ROOT/deploy/local/bootstrap.sh"; exit; fi
python3 - "$provider" "$ENVY_STATE_DIR/kind.json" "$ENVY_PREVIEW_PORT" "$ENVY_API_PORT" "$(version node_image)" "${ENVY_TEST_ENFORCE_POLICY:-0}" <<'PY'
import json,sys
provider,path,preview,api,image,enforce=sys.argv[1:]
network={'apiServerAddress':'127.0.0.1'}
if provider=='cilium' or enforce=='1': network.update(disableDefaultCNI=True,kubeProxyMode='none')
json.dump({'kind':'Cluster','apiVersion':'kind.x-k8s.io/v1alpha4','networking':network,'nodes':[{'role':'control-plane','image':image,'extraPortMappings':[{'containerPort':30080,'hostPort':int(preview),'listenAddress':'127.0.0.1'},{'containerPort':30081,'hostPort':int(api),'listenAddress':'127.0.0.1'}]}]},open(path,'w'))
PY
kind create cluster --name "$ENVY_CLUSTER_NAME" --config "$ENVY_STATE_DIR/kind.json" --kubeconfig "$KUBECONFIG" --wait 0s
kubectl apply --server-side -f "https://github.com/kubernetes-sigs/gateway-api/releases/download/v$(version gateway_api)/standard-install.yaml"
if [[ "$provider" == cilium || ${ENVY_TEST_ENFORCE_POLICY:-0} == 1 ]]; then
 gateway=false; [[ "$provider" != cilium ]] || gateway=true
 helm upgrade --install cilium cilium --repo https://helm.cilium.io --version "$(version cilium)" -n kube-system \
  --set kubeProxyReplacement=true --set k8sServiceHost="$ENVY_CLUSTER_NAME-control-plane" --set k8sServicePort=6443 \
  --set l7Proxy=true --set gatewayAPI.enabled="$gateway" --set gatewayAPI.hostNetwork.enabled=false \
  --set socketLB.hostNamespaceOnly=true --set cni.exclusive=false \
  --set operator.replicas=1 --wait --timeout 8m
fi
if [[ "$provider" == istio ]]; then bash "$ENVY_ROOT/deploy/local/bootstrap.sh"; exit; fi
if [[ "$provider" == cilium ]]; then
 # Helm readiness precedes the operator's asynchronous CRD registration.
 kubectl wait --for=create crd/ciliumloadbalancerippools.cilium.io --timeout=180s
 kubectl wait --for=condition=Established crd/ciliumloadbalancerippools.cilium.io --timeout=60s
 kubectl apply -f - <<'YAML'
apiVersion: cilium.io/v2
kind: CiliumLoadBalancerIPPool
metadata:
  name: envy-test-pool
spec:
  blocks:
    - cidr: 192.0.2.10/32
YAML
else
 helm upgrade --install linkerd-crds linkerd-crds --repo https://helm.linkerd.io/edge --version "$(version linkerd_chart)" -n linkerd --create-namespace
 # Test-only trust root and issuer; never used in production examples.
 openssl ecparam -name prime256v1 -genkey -noout -out "$ENVY_STATE_DIR/root.key"
 openssl req -new -x509 -key "$ENVY_STATE_DIR/root.key" -out "$ENVY_STATE_DIR/root.crt" -days 2 -subj /CN=root.linkerd.cluster.local
 openssl ecparam -name prime256v1 -genkey -noout -out "$ENVY_STATE_DIR/issuer.key"
 openssl req -new -key "$ENVY_STATE_DIR/issuer.key" -out "$ENVY_STATE_DIR/issuer.csr" -subj /CN=identity.linkerd.cluster.local
 printf 'basicConstraints=critical,CA:TRUE\nkeyUsage=critical,keyCertSign,cRLSign\n' > "$ENVY_STATE_DIR/issuer.ext"
 openssl x509 -req -in "$ENVY_STATE_DIR/issuer.csr" -CA "$ENVY_STATE_DIR/root.crt" -CAkey "$ENVY_STATE_DIR/root.key" -CAcreateserial -out "$ENVY_STATE_DIR/issuer.crt" -days 1 -extfile "$ENVY_STATE_DIR/issuer.ext"
 helm upgrade --install linkerd-control-plane linkerd-control-plane --repo https://helm.linkerd.io/edge --version "$(version linkerd_chart)" -n linkerd \
  --set-string controllerImageVersion="$(version linkerd)@$(digest cr.l5d.io/linkerd/controller:$(version linkerd))" \
  --set-string proxy.image.version="$(version linkerd)@$(digest cr.l5d.io/linkerd/proxy:$(version linkerd))" \
  --set-file identityTrustAnchorsPEM="$ENVY_STATE_DIR/root.crt" --set-file identity.issuer.tls.crtPEM="$ENVY_STATE_DIR/issuer.crt" \
  --set-file identity.issuer.tls.keyPEM="$ENVY_STATE_DIR/issuer.key" --wait --timeout 8m
 kubectl apply --server-side -f "https://github.com/envoyproxy/gateway/releases/download/v$(version envoy_gateway)/envoy-gateway-crds.yaml"
 helm upgrade --install eg oci://docker.io/envoyproxy/gateway-helm --version "v$(version envoy_gateway)" -n envoy-gateway-system --create-namespace --skip-crds --set crds.enabled=false --set-string global.images.envoyGateway.image="docker.io/envoyproxy/gateway:v$(version envoy_gateway)@$(digest docker.io/envoyproxy/gateway:v$(version envoy_gateway))" --wait --timeout 8m
fi
kubectl wait nodes --all --for=condition=Ready --timeout=180s
if kubectl get crd virtualservices.networking.istio.io >/dev/null 2>&1; then echo 'Non-Istio fixture unexpectedly contains Istio' >&2; exit 1; fi
kubectl get pods -A -o json > "$ENVY_STATE_DIR/controller-pods.json"
