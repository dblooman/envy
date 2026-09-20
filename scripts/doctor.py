#!/usr/bin/env python3
"""Read-only prerequisite checks. --local checks only the Kubernetes quickstart."""

import argparse
import json
import os
from pathlib import Path
import platform
import re
import shutil
import socket
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]


def run(*args):
    return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT).strip()


def version(value):
    match = re.search(r"(\d+)\.(\d+)(?:\.(\d+))?", value)
    if not match:
        raise ValueError("unrecognized version")
    return tuple(int(part or 0) for part in match.groups())


def port_available(port):
    with socket.socket() as sock:
        try:
            sock.bind(("127.0.0.1", port))
            return True
        except OSError:
            return False


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--local", action="store_true")
    args = parser.parse_args()
    errors = []
    tools = {
        "go": "install the Go version in go.mod",
        "docker": "install Docker Desktop or Docker Engine",
        "kubectl": "install kubectl",
        "curl": "install curl",
    }
    if not args.local:
        tools.update(
            {
                "node": "install Node from .nvmrc",
                "pnpm": "npm install --global pnpm@10.20.0",
                "helm": "install Helm 3.19.0 or newer for chart checks",
            }
        )
    for tool, hint in tools.items():
        if not shutil.which(tool):
            errors.append(f"Missing {tool}: {hint}.")
    if platform.system() not in (
        "Darwin",
        "Linux",
    ) or platform.machine().lower() not in ("arm64", "aarch64", "x86_64", "amd64"):
        errors.append(
            "Local clusters support macOS/Linux on arm64/amd64. On Windows use WSL2 with Docker integration."
        )
    checks = [
        (
            "go",
            ["version"],
            lambda v: v
            >= version(re.search(r"^go (.+)$", (ROOT / "go.mod").read_text(), re.M)[1]),
            "use Go from go.mod or newer",
        )
    ]
    if not args.local:
        checks += [
            (
                "node",
                ["--version"],
                lambda v: (24, 8, 0) <= v < (25, 0, 0),
                "use Node 24.8.0 or a newer Node 24 patch",
            ),
            ("pnpm", ["--version"], lambda v: v == (10, 20, 0), "use pnpm 10.20.0"),
            (
                "helm",
                ["version", "--short"],
                lambda v: (3, 19, 0) <= v < (4, 0, 0),
                "use Helm 3.19.0 or a newer Helm 3 patch",
            ),
        ]
    for tool, flags, supported, hint in checks:
        if shutil.which(tool):
            try:
                if not supported(version(run(tool, *flags))):
                    errors.append(f"Unsupported {tool} version: {hint}.")
            except (subprocess.SubprocessError, ValueError):
                errors.append(f"Cannot read {tool} version: {hint}.")
    owned_ports = set()
    if os.environ.get("DOCKER_BUILDKIT") == "0":
        errors.append(
            "Docker BuildKit is disabled: unset DOCKER_BUILDKIT or set it to 1 for cached builds."
        )
    if shutil.which("docker"):
        try:
            run("docker", "buildx", "version")
        except subprocess.SubprocessError:
            errors.append(
                "Docker Buildx is missing: install the Docker Buildx plugin (included in Docker Desktop)."
            )
        try:
            run("docker", "info", "--format", "{{.ServerVersion}}")
            cluster = os.environ.get("ENVY_CLUSTER_NAME", "envy-dev")
            if not re.fullmatch(r"envy-[a-z0-9-]+", cluster):
                errors.append(
                    "ENVY_CLUSTER_NAME must start with envy- and contain only lowercase letters, digits, and hyphens."
                )
            containers = run(
                "docker",
                "ps",
                "-q",
                "--filter",
                f"label=io.x-k8s.kind.cluster={cluster}",
            ).split()
            if containers:
                for container in json.loads(run("docker", "inspect", *containers)):
                    for bindings in (
                        container.get("NetworkSettings", {}).get("Ports", {}).values()
                    ):
                        owned_ports.update(
                            int(b["HostPort"])
                            for b in bindings or []
                            if b["HostIp"] == "127.0.0.1"
                        )
        except (subprocess.SubprocessError, ValueError, KeyError):
            errors.append(
                "Docker is unavailable: start Docker and wait until docker info succeeds."
            )
    ports = []
    for name, default in [("ENVY_PREVIEW_PORT", "8080"), ("ENVY_API_PORT", "8081")]:
        try:
            port = int(os.environ.get(name, default))
            if not 1 <= port <= 65535:
                raise ValueError()
            ports.append(port)
            if port not in owned_ports and not port_available(port):
                errors.append(
                    f"Port {port} is occupied: stop its owner or choose another {name}."
                )
        except ValueError:
            errors.append(f"{name} must be a port between 1 and 65535.")
    if len(ports) == 2 and ports[0] == ports[1]:
        errors.append("ENVY_PREVIEW_PORT and ENVY_API_PORT must be different.")
    for error in errors:
        print(f"ERROR: {error}", file=sys.stderr)
    if errors:
        return 1
    print("Prerequisites passed. No cluster or configuration was changed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
