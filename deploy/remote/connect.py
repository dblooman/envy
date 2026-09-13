#!/usr/bin/env python3
"""Connect local Envy clients to a dedicated development Mac over SSH."""
import argparse
import json
import os
from pathlib import Path
import shlex
import subprocess
from urllib.parse import urlsplit


def port(value):
    result = int(value)
    if not 1024 <= result <= 65535:
        raise argparse.ArgumentTypeError("use a port between 1024 and 65535")
    return result


def forwarded_config(config, local_port):
    """Preserve TLS verification and credentials, changing only the dial port."""
    if len(config.get("clusters", [])) != 1:
        raise ValueError("expected one minified Kubernetes cluster")
    cluster = config["clusters"][0]["cluster"]
    endpoint = urlsplit(cluster["server"])
    if endpoint.scheme != "https" or endpoint.hostname != "127.0.0.1" or not endpoint.port:
        raise ValueError("remote kind API must be an HTTPS loopback endpoint")
    if cluster.get("insecure-skip-tls-verify"):
        raise ValueError("remote kubeconfig disables certificate verification")
    if not cluster.get("certificate-authority-data"):
        raise ValueError("remote kubeconfig must embed its CA certificate")
    for user in config.get("users", []):
        if "exec" in user.get("user", {}) or "auth-provider" in user.get("user", {}):
            raise ValueError("expected static kind credentials, not executable authentication")
    cluster["server"] = f"https://127.0.0.1:{local_port}"
    return config, endpoint.port


def remote_command(checkout, command):
    # SSH passes commands to a shell. Quote every path and argument explicitly.
    return "cd " + shlex.quote(checkout) + " && PATH=/opt/homebrew/bin:/usr/local/bin:$PATH " + shlex.join(command)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", required=True, help="SSH host or user@host; normal SSH configuration applies")
    parser.add_argument("--checkout", required=True, help="absolute repository path on the remote Mac")
    parser.add_argument("--preview-port", type=port, default=18080)
    parser.add_argument("--api-port", type=port, default=18081)
    parser.add_argument("--kubernetes-port", type=port, default=18443)
    args = parser.parse_args()
    if args.host.startswith("-") or any(c.isspace() for c in args.host):
        parser.error("invalid SSH host")
    if not args.checkout.startswith("/"):
        parser.error("--checkout must be absolute")
    if len({args.preview_port, args.api_port, args.kubernetes_port}) != 3:
        parser.error("forwarded ports must be distinct")
    root = Path(__file__).resolve().parents[2]
    state = root / ".envy" / "remote"
    os.umask(0o077)
    state.mkdir(parents=True, exist_ok=True)
    state.chmod(0o700)
    # SSH host-key checking is left enabled. Authentication and server identity
    # are handled by the user's normal SSH configuration.
    def read(command):
        return subprocess.check_output(["ssh", args.host, remote_command(args.checkout, command)])
    config = json.loads(read(["kubectl", "--kubeconfig", ".envy/envy-dev/kubeconfig", "config", "view", "--raw", "--minify", "--flatten", "-o", "json"]))
    config, remote_kubernetes_port = forwarded_config(config, args.kubernetes_port)
    token = read(["cat", ".envy/envy-dev/api-token"])
    if len(token.strip()) < 32:
        raise ValueError("remote API token is missing or invalid")
    files = {"kubeconfig": json.dumps(config).encode(), "api-token": token}
    environment = {
        "KUBECONFIG": str(state / "kubeconfig"),
        "ENVY_API_URL": f"http://127.0.0.1:{args.api_port}",
        "ENVY_API_TOKEN_FILE": str(state / "api-token"),
        "ENVY_REMOTE_PREVIEW_PORT": str(args.preview_port),
    }
    files["client.env"] = "".join(f"export {key}={shlex.quote(value)}\n" for key, value in environment.items()).encode()
    for name, data in files.items():
        path = state / name
        # Refuse symlinks instead of following them when writing credentials.
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "wb") as output:
            os.fchmod(output.fileno(), 0o600)
            output.write(data)
    command = ["ssh", "-N", "-T", "-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3"]
    for local, remote in [(args.preview_port, args.preview_port), (args.api_port, args.api_port), (args.kubernetes_port, remote_kubernetes_port)]:
        command += ["-L", f"127.0.0.1:{local}:127.0.0.1:{remote}"]
    command.append(args.host)
    print(f"Connecting. In another terminal: source {shlex.quote(str(state / 'client.env'))}", flush=True)
    print(f"Website: http://127.0.0.1:{args.api_port} — leave this tunnel running; Ctrl-C disconnects.", flush=True)
    try:
        return subprocess.call(command)
    except KeyboardInterrupt:
        return 130


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (ValueError, subprocess.CalledProcessError) as exc:
        raise SystemExit(str(exc))
