#!/usr/bin/env bash
# Attach an operator-supplied GitHub App key to the dedicated local control plane.
set -euo pipefail
source "$(dirname "$0")/common.sh"
umask 077
: "${ENVY_GITHUB_APP_ID:?Set ENVY_GITHUB_APP_ID}"
: "${ENVY_GITHUB_APP_PRIVATE_KEY_FILE:?Set ENVY_GITHUB_APP_PRIVATE_KEY_FILE}"
[[ "$ENVY_GITHUB_APP_ID" =~ ^[0-9]+$ ]] || { echo 'App ID must be numeric' >&2; exit 1; }
openssl rsa -in "$ENVY_GITHUB_APP_PRIVATE_KEY_FILE" -check -noout >/dev/null 2>&1
kubectl -n envy-system create secret generic envy-github-app \
  --from-file=private-key.pem="$ENVY_GITHUB_APP_PRIVATE_KEY_FILE" \
  --dry-run=client -o json | kubectl apply -f -
python3 - "$ENVY_GITHUB_APP_ID" "$ENVY_STATE_DIR/github-app-patch.json" <<'PY'
import json, sys
from pathlib import Path
spec = {
    'containers': [{'name': 'server', 'env': [
        {'name': 'ENVY_GITHUB_APP_ID', 'value': sys.argv[1]},
        {'name': 'ENVY_GITHUB_APP_PRIVATE_KEY_FILE', 'value': '/github-app/private-key.pem'},
    ], 'volumeMounts': [{'name': 'github-app', 'mountPath': '/github-app', 'readOnly': True}]}],
    'volumes': [{'name': 'github-app', 'secret': {
        'secretName': 'envy-github-app', 'defaultMode': 0o440,
        'items': [{'key': 'private-key.pem', 'path': 'private-key.pem'}],
    }}],
}
Path(sys.argv[2]).write_text(json.dumps({'spec': {'template': {'spec': spec}}}))
PY
kubectl -n envy-system patch deployment envy-server --type=strategic \
  --patch-file "$ENVY_STATE_DIR/github-app-patch.json"
# Also reload when only the secret changed; the server reads its key at startup.
kubectl -n envy-system rollout restart deployment/envy-server
kubectl -n envy-system rollout status deployment/envy-server --timeout=180s
