#!/usr/bin/env python3
"""Cluster-side private registry gate: anonymous pull fails; distributed pull works."""

import json
from pathlib import Path
import subprocess
import time
import uuid
from render import load_builds


def main():
    image = load_builds("deploy/lan/builds.json")["pricing_branch"]["image"]
    kube = ["kubectl", "--context=docker-desktop"]

    def run(*args, body=None):
        return subprocess.check_output(
            kube + list(args), input=None if body is None else json.dumps(body).encode()
        )

    owned = []
    report = {"image": image, "result": "failed", "observations": {}}
    try:
        for authorized in (False, True):
            ns = "envy-pull-" + uuid.uuid4().hex[:12]
            metadata = {"name": ns}
            if authorized:
                metadata["labels"] = {"envy.dev/installation": "envy-lan"}
            run(
                "create",
                "-f",
                "-",
                body={"apiVersion": "v1", "kind": "Namespace", "metadata": metadata},
            )
            owned.append(ns)
            if authorized:
                deadline = time.monotonic() + 60
                while not run(
                    "-n",
                    ns,
                    "get",
                    "secret",
                    "envy-ghcr",
                    "--ignore-not-found",
                    "-o",
                    "name",
                ).strip():
                    if time.monotonic() > deadline:
                        raise RuntimeError("registry Secret was not distributed")
                    time.sleep(1)
            spec = {
                "automountServiceAccountToken": False,
                "restartPolicy": "Never",
                "containers": [
                    {
                        "name": "pricing",
                        "image": image,
                        "imagePullPolicy": "Always",
                        "env": [{"name": "SHOP_ROLE", "value": "pricing"}],
                        "readinessProbe": {
                            "httpGet": {"path": "/readyz", "port": 8080}
                        },
                        "resources": {
                            "requests": {"cpu": "10m", "memory": "32Mi"},
                            "limits": {"cpu": "200m", "memory": "128Mi"},
                        },
                    }
                ],
            }
            if authorized:
                spec["imagePullSecrets"] = [{"name": "envy-ghcr"}]
            run(
                "create",
                "-f",
                "-",
                body={
                    "apiVersion": "v1",
                    "kind": "Pod",
                    "metadata": {"name": "pull-check", "namespace": ns},
                    "spec": spec,
                },
            )
            deadline = time.monotonic() + 120
            while True:
                pod = json.loads(
                    run("-n", ns, "get", "pod", "pull-check", "-o", "json")
                )
                statuses = pod.get("status", {}).get("containerStatuses", [])
                status = statuses[0] if statuses else {}
                reason = status.get("state", {}).get("waiting", {}).get("reason")
                ready = status.get("ready", False)
                if not authorized and ready:
                    raise RuntimeError(
                        "anonymous pull succeeded: image is public or node credentials bypass this test"
                    )
                message = (
                    status.get("state", {})
                    .get("waiting", {})
                    .get("message", "")
                    .lower()
                )
                denied = any(
                    word in message
                    for word in (
                        "unauthorized",
                        "401",
                        "403",
                        "denied",
                        "authentication required",
                    )
                )
                if (authorized and ready) or (
                    not authorized
                    and denied
                    and reason in ("ErrImagePull", "ImagePullBackOff")
                ):
                    report["observations"][
                        "authorized" if authorized else "anonymous"
                    ] = {
                        "ready": ready,
                        "reason": reason,
                        "image_id": status.get("imageID"),
                    }
                    break
                if time.monotonic() > deadline:
                    raise RuntimeError(
                        f"pull gate timed out: authorized={authorized}, reason={reason}"
                    )
                time.sleep(1)
        report["result"] = "passed"
    finally:
        cleanup_errors = []
        for ns in owned:
            if subprocess.run(
                kube + ["delete", "namespace", ns, "--wait=true", "--timeout=60s"],
                check=False,
            ).returncode:
                cleanup_errors.append(ns)
        if cleanup_errors:
            report["result"] = "failed"
            report["cleanup_errors"] = cleanup_errors
        output = Path(".envy/lan/private-pull.json")
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(report, indent=2) + "\n")
        if cleanup_errors:
            raise RuntimeError(
                "Private-pull fixture cleanup incomplete; inspect report"
            )


if __name__ == "__main__":
    main()
