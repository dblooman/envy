#!/usr/bin/env python3
"""Render borrowed workloads and the API catalog from recorded immutable builds."""
import argparse
import json
from pathlib import Path
import re


def load_builds(path):
    builds = json.loads(Path(path).read_text())
    for name in ("storefront_main", "pricing_main", "pricing_branch"):
        item = builds[name]
        repo = "envy-test-storefront" if name == "storefront_main" else "envy-test-pricing"
        if not re.fullmatch(r"ghcr.io/dblooman/" + repo + r"@sha256:[a-f0-9]{64}", item["image"]):
            raise ValueError(f"invalid immutable image for {name}")
        if not re.fullmatch(r"[a-f0-9]{40}", item["revision"]) or not str(item["run_id"]).isdigit():
            raise ValueError(f"missing source/run provenance for {name}")
    if builds["pricing_main"]["revision"] == builds["pricing_branch"]["revision"] or builds["pricing_main"]["image"] == builds["pricing_branch"]["image"]:
        raise ValueError("branch must contain a different commit and image")
    return builds


def render(builds):
    ns = "envy-lan-baseline"
    objects = [{"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": ns, "labels": {"istio-injection": "enabled"}}}]
    catalog = {"api_version": "envy/v1", "project": {"id": "lan", "name": "LAN APIs"}, "components": [], "baseline": {
        "id": "staging", "project": "lan", "revision": "main-" + builds["storefront_main"]["revision"][:12] + "-" + builds["pricing_main"]["revision"][:12],
        "endpoint": "http://baseline.envy.test:30080", "routing": {"namespace": ns, "gateway": "envy-preview", "entry_component": "storefront"},
        "verification": {"kind": "http", "path": "/products", "expected_status": 200}, "components": {}}}
    for name in ("storefront", "pricing"):
        image = builds[name + "_main"]["image"]
        env = {"SHOP_ROLE": name}
        if name == "storefront":
            env["DOWNSTREAM_URL"] = f"http://pricing.{ns}.svc.cluster.local:8080/products"
        labels = {"app": name, "envy.dev/composition": "baseline"}
        metadata = {"name": name, "namespace": ns}
        container = {"name": name, "image": image, "imagePullPolicy": "IfNotPresent", "ports": [{"name": "http", "containerPort": 8080}],
                     "env": [{"name": k, "value": v} for k, v in env.items()] + [{"name": "POD_UID", "valueFrom": {"fieldRef": {"fieldPath": "metadata.uid"}}}],
                     "resources": {"requests": {"cpu": "10m", "memory": "32Mi"}, "limits": {"cpu": "200m", "memory": "128Mi"}},
                     "readinessProbe": {"httpGet": {"path": "/readyz", "port": "http"}},
                     "securityContext": {"runAsNonRoot": True, "runAsUser": 65532, "allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True, "capabilities": {"drop": ["ALL"]}}}
        objects += [{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": metadata, "spec": {"replicas": 1, "selector": {"matchLabels": labels}, "template": {"metadata": {"labels": labels}, "spec": {"automountServiceAccountToken": False, "imagePullSecrets": [{"name": "envy-ghcr"}], "containers": [container]}}}},
                    {"apiVersion": "v1", "kind": "Service", "metadata": metadata, "spec": {"selector": labels, "ports": [{"name": "http", "appProtocol": "http", "port": 8080, "targetPort": "http"}]}}]
        catalog["components"].append({"id": name, "project": "lan", "protocol": "http", "port": 8080, "profile": "http-small", "health_path": "/healthz", "readiness_path": "/readyz", "overridable": name == "pricing", "env": env, "image_pull_secrets": ["envy-ghcr"]})
        catalog["baseline"]["components"][name] = {"service_host": f"{name}.{ns}.svc.cluster.local", "port": 8080, "image": image}
    objects += [{"apiVersion": "networking.istio.io/v1", "kind": "Gateway", "metadata": {"name": "envy-preview", "namespace": ns}, "spec": {"selector": {"istio": "ingressgateway"}, "servers": [{"port": {"number": 80, "name": "http", "protocol": "HTTP"}, "hosts": ["*.envy.test"]}]}},
                {"apiVersion": "networking.istio.io/v1", "kind": "VirtualService", "metadata": {"name": "baseline", "namespace": ns}, "spec": {"hosts": ["baseline.envy.test"], "gateways": ["envy-preview"], "http": [{"headers": {"request": {"remove": ["baggage"]}}, "route": [{"destination": {"host": f"storefront.{ns}.svc.cluster.local", "port": {"number": 8080}}}]}]}}]
    return {"apiVersion": "v1", "kind": "List", "items": objects}, catalog


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--builds", required=True)
    parser.add_argument("--output", default=".envy/lan")
    args = parser.parse_args()
    workload, catalog = render(load_builds(args.builds))
    output = Path(args.output); output.mkdir(parents=True, exist_ok=True)
    for name, body in [("baseline.json", workload), ("catalog.json", catalog)]:
        (output / name).write_text(json.dumps(body, indent=2) + "\n")
