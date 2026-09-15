#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
trap diagnostics ERR
umask 077
python3 - "$ENVY_STATE_DIR" <<'PY'
from pathlib import Path
import secrets,sys
root=Path(sys.argv[1])
for name in ['postgres-password']:
 p=root/name
 if not p.exists(): p.write_text(secrets.token_hex(32))
 p.chmod(0o600)
(root/'database-url').write_text('postgres://envy:'+ (root/'postgres-password').read_text().strip() +'@envy-postgres.envy-system.svc.cluster.local:5432/envy?sslmode=disable')
(root/'database-url').chmod(0o600)
PY
kubectl create namespace envy-system --dry-run=client -o yaml | kubectl apply -f -
kubectl -n envy-system create secret generic envy-credentials --from-file=postgres-password="$ENVY_STATE_DIR/postgres-password" --from-file=database-url="$ENVY_STATE_DIR/database-url" --dry-run=client -o yaml | kubectl apply -f -
docker build -f "$ENVY_ROOT/Dockerfile.server" -t envy/server:dev "$ENVY_ROOT"
kind load docker-image --name "$ENVY_CLUSTER_NAME" envy/server:dev
# Never rewrite the checked-in manifest for machine-local endpoint ports.
sed "s|http://envy.localhost:8080|http://envy.localhost:$ENVY_PREVIEW_PORT|g" "$ENVY_ROOT/deploy/kubernetes/server.yaml" > "$ENVY_STATE_DIR/server.yaml"
# The checked-in Kubernetes manifest remains token-authenticated. Only this
# explicitly local startup path selects automatic Admin access.
python3 - "$ENVY_STATE_DIR/server.yaml" <<'PYDEV'
from pathlib import Path
import sys
p = Path(sys.argv[1])
s = p.read_text().replace('name: ENVY_API_TOKEN_FILE\n              value: /secrets/api-token', 'name: ENVY_AUTH_MODE\n              value: dev')
s = s.replace('              - key: api-token\n                path: api-token', '              - key: database-url\n                path: database-url')
p.write_text(s)
PYDEV
kubectl apply -f "$ENVY_STATE_DIR/server.yaml"
kubectl -n envy-system rollout status deployment/envy-postgres --timeout=180s
# An unchanged local tag still needs a restart after a binary rebuild.
kubectl -n envy-system rollout restart deployment/envy-server
kubectl -n envy-system rollout status deployment/envy-server --timeout=180s
mkdir -p "$ENVY_ROOT/.envy/bin"
go build -o "$ENVY_ROOT/.envy/bin/envy-mcp" "$ENVY_ROOT/cmd/mcp"
go build -o "$ENVY_ROOT/.envy/bin/delivery" "$ENVY_ROOT/cmd/delivery"
kubectl get pods -A -o jsonpath='{range .items[*]}{range .status.containerStatuses[*]}{.image}{" "}{.imageID}{"\n"}{end}{end}' > "$ENVY_STATE_DIR/running-images.txt"
echo "Envy API: http://127.0.0.1:$ENVY_API_PORT; dev mode runs as Admin"
