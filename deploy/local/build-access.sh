#!/usr/bin/env bash
# Configure registry reads and scoped build reporting in the dedicated kind cluster.
set -euo pipefail
source "$(dirname "$0")/common.sh"
umask 077
: "${ENVY_REGISTRY_CONFIG_FILE:?Set ENVY_REGISTRY_CONFIG_FILE to a Docker config.json}"
: "${ENVY_BUILD_CREDENTIALS_FILE:?Set ENVY_BUILD_CREDENTIALS_FILE}"
python3 - "$ENVY_REGISTRY_CONFIG_FILE" "$ENVY_BUILD_CREDENTIALS_FILE" "$ENVY_STATE_DIR" <<'PY'
import json, subprocess, sys
from pathlib import Path
registry = json.loads(Path(sys.argv[1]).read_text())
credentials = json.loads(Path(sys.argv[2]).read_text())
if not registry.get('auths') or registry.get('credsStore') or registry.get('credHelpers'):
    raise SystemExit('Use inline registry auth; credential helpers are unavailable in the server image')
if not isinstance(credentials, list) or not credentials:
    raise SystemExit('Build credentials must be a nonempty JSON array')
state = Path(sys.argv[3])
spec = {
    'containers': [{'name': 'server', 'env': [
        {'name': 'DOCKER_CONFIG', 'value': '/build-access'},
        {'name': 'ENVY_BUILD_CREDENTIALS_FILE', 'value': '/build-access/build-credentials.json'},
    ], 'volumeMounts': [{'name': 'build-access', 'mountPath': '/build-access', 'readOnly': True}]}],
    'volumes': [{'name': 'build-access', 'secret': {'secretName': 'envy-build-access', 'defaultMode': 0o440}}],
}
(state / 'build-access-patch.json').write_text(json.dumps({'spec': {'template': {'spec': spec}}}))
PY
kubectl -n envy-system create secret generic envy-build-access \
  --from-file=config.json="$ENVY_REGISTRY_CONFIG_FILE" \
  --from-file=build-credentials.json="$ENVY_BUILD_CREDENTIALS_FILE" \
  --dry-run=client -o json | kubectl apply -f -
# kind's node credential approach lets newly created composition namespaces pull
# without distributing registry secrets into each application namespace.
for node in $(kind get nodes --name "$ENVY_CLUSTER_NAME"); do
  python3 - "$node" "$ENVY_REGISTRY_CONFIG_FILE" "$ENVY_STATE_DIR" <<'PY'
import json, subprocess, sys, tempfile
from pathlib import Path
node, source, state = sys.argv[1:]
result = subprocess.run(['docker', 'exec', node, 'cat', '/var/lib/kubelet/config.json'], capture_output=True, text=True)
if result.returncode and 'No such file' not in result.stderr:
    raise SystemExit('Cannot inspect existing node registry configuration')
config = json.loads(result.stdout) if result.returncode == 0 else {}
config.setdefault('auths', {}).update(json.loads(Path(source).read_text())['auths'])
with tempfile.NamedTemporaryFile(mode='w', dir=state) as out:
    json.dump(config, out)
    out.flush()
    subprocess.run(['docker', 'cp', out.name, node + ':/var/lib/kubelet/config.json'], check=True)
subprocess.run(['docker', 'exec', node, 'chmod', '600', '/var/lib/kubelet/config.json'], check=True)
subprocess.run(['docker', 'exec', node, 'systemctl', 'restart', 'kubelet.service'], check=True)
PY
done
kubectl -n envy-system patch deployment envy-server --type=strategic \
  --patch-file "$ENVY_STATE_DIR/build-access-patch.json"
kubectl -n envy-system rollout restart deployment/envy-server
kubectl -n envy-system rollout status deployment/envy-server --timeout=180s
