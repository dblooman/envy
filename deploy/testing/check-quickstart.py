#!/usr/bin/env python3
"""Render the packaged quickstart and verify its installation contract."""

import json
import re
import subprocess

chart = "deploy/helm/envy-quickstart"
command = [
    "helm",
    "template",
    "envy",
    chart,
    "--namespace",
    "envy-quickstart",
    "--include-crds",
]
rendered = subprocess.check_output(command, text=True)
assert (
    "helm.sh/hook" not in rendered
), "Bundled DB must migrate through an init container"
assert "initContainers:" in rendered and "name: migrate" in rendered
assert "image: davey/envy:" in rendered
assert "command: [/envy-quickstart]" in rendered
assert "resourceNames: [istio-sidecar-injector-envy-quickstart]" in rendered
assert rendered.count("helm.sh/resource-policy: keep") >= 2
assert "kind: PersistentVolumeClaim" in rendered
assert "name: envy-ingress" in rendered
crds = []
for doc in rendered.split("\n---"):
    if "\nkind: CustomResourceDefinition\n" in doc:
        crds.append(re.search(r"\n  name: (\S+)", doc).group(1))
    if "catalog.json: |" in doc:
        catalog = json.loads(doc.split("catalog.json: |", 1)[1])
        assert re.fullmatch("[a-z0-9-]+", catalog["baseline"]["revision"])
        assert catalog["baseline"]["routing"]["gateway"] == "envy-preview"
        assert catalog["baseline"]["routing"]["namespace"] == "envy-quickstart-shop"
        assert catalog["baseline"]["components"]["pricing"]["image"].endswith("-v1")
    if "config.json: |-" in doc:
        config = json.loads(doc.split("config.json: |-", 1)[1])
        assert config["istio"]["injection_labels"] == {"istio.io/rev": "default"}
        assert config["auth"]["external_origin"] == "http://127.0.0.1:8080"
assert crds and len(crds) == len(set(crds)), "CRDs must be installed exactly once"
assert 'hosts: ["127.0.0.1"]' in rendered, "Dashboard must not claim preview hosts"
for args in (["--namespace", "default"], ["--set", "envy.migrations.mode=invalid"]):
    assert subprocess.run(command + args, capture_output=True).returncode != 0
standard = subprocess.check_output(
    ["helm", "template", "envy", "deploy/helm/envy", "--set", "installationID=test"],
    text=True,
)
assert "pre-install,pre-upgrade" in standard
print("Quickstart CRDs, migrations, catalog, RBAC and installation guards checked")
