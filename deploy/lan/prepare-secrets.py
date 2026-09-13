#!/usr/bin/env python3
"""Run on the cluster Mac. Never emits credentials to stdout or command arguments."""
import base64
import json
import os
from pathlib import Path
import secrets
import subprocess


def main():
    state = Path(".envy/lan")
    os.umask(0o077)
    state.mkdir(parents=True, exist_ok=True)
    kube = ["kubectl", "--context=docker-desktop"]
    namespace = {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": "envy-system"}}
    subprocess.run(kube + ["apply", "-f", "-"], input=json.dumps(namespace).encode(), check=True)
    def stored(name, secret_name):
        path = state / name
        if not path.exists():
            existing = subprocess.check_output(kube + ["-n", "envy-system", "get", "secret", secret_name, "--ignore-not-found", "-o", "name"])
            if existing.strip():
                raise SystemExit(f"Restore the original {path} before reinstalling; existing {secret_name} will not be rotated")
            with path.open("x") as out:
                out.write(secrets.token_hex(32))
        path.chmod(0o600)
        return path.read_text().strip()
    password, token = stored("postgres-password", "envy-database"), stored("api-token", "envy-machines")
    # Read an explicit Docker config containing only the GHCR credential. No
    # implicit copying of the user's entire Docker credential configuration.
    registry = json.loads(Path(os.environ["ENVY_GHCR_CONFIG_FILE"]).read_text())
    if set(registry) != {"auths"} or set(registry["auths"]) != {"ghcr.io"} or not registry["auths"]["ghcr.io"].get("auth"):
        raise SystemExit('Provide a Docker config with only auths.ghcr.io.auth')
    base64.b64decode(registry["auths"]["ghcr.io"]["auth"], validate=True)
    records = [
        ("envy-database", "Opaque", {"password": password, "url": f"postgres://envy:{password}@envy-postgres.envy-system.svc.cluster.local:5432/envy?sslmode=disable"}),
        ("envy-machines", "Opaque", {"credentials.json": json.dumps([{"id": "lan-client", "display_name": "LAN acceptance client", "token": token}])}),
        ("envy-ghcr", "kubernetes.io/dockerconfigjson", {".dockerconfigjson": json.dumps(registry)}),
    ]
    for name, kind, data in records:
        # Never rotate an existing database password or caller token implicitly.
        existing = subprocess.check_output(kube + ["-n", "envy-system", "get", "secret", name, "--ignore-not-found", "-o", "name"])
        if existing.strip():
            print(f"Retaining existing Secret {name}; rotation is explicit")
            continue
        obj = {"apiVersion": "v1", "kind": "Secret", "metadata": {"name": name, "namespace": "envy-system"}, "type": kind, "stringData": data}
        result = subprocess.run(kube + ["create", "-f", "-"], input=json.dumps(obj).encode(), stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        if result.returncode:
            raise SystemExit(f"Could not create {name}; inspect cluster access (credential payload omitted)")
    print("Secret setup complete. Retained Secrets may differ from local files; use the original API token if this is a reinstall.")


if __name__ == "__main__":
    main()
