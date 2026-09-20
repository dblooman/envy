#!/usr/bin/env python3
"""Upgrade a populated, pre-binding Istio installation and retain live traffic."""

import json, os, pathlib, subprocess, time, urllib.request, urllib.parse

root = pathlib.Path(__file__).resolve().parents[2]
state = pathlib.Path(os.environ["ENVY_STATE_DIR"])
base = os.environ["ENVY_API_URL"]
token = (state / "api-token").read_text().strip()


def request(path, method="GET", body=None):
    req = urllib.request.Request(
        base + path,
        method=method,
        data=None if body is None else json.dumps(body).encode(),
        headers={
            "Authorization": "Bearer " + token,
            "Content-Type": "application/json",
        },
    )
    with urllib.request.urlopen(req, timeout=10) as res:
        return json.load(res)


def wait(id, phase):
    last = None
    for attempt in range(120):
        try:
            last = request("/v1/compositions/" + id)
            if last["phase"] == phase:
                return last
        except Exception as e:
            last = str(e)
        time.sleep(2)
    raise SystemExit(f"Upgrade composition did not reach {phase}: {last}")


def traffic(url, composition, leaf):
    u = urllib.parse.urlparse(url)
    args = [
        "curl",
        "--fail",
        "--silent",
        "--show-error",
        "--max-time",
        "10",
        "--resolve",
        f"{u.hostname}:{u.port}:127.0.0.1",
        url,
    ]
    if u.scheme == "https":
        args.extend(["--cacert", str(state / "preview.crt")])
    result = json.loads(subprocess.check_output(args, text=True))["chain"]
    assert [x["version"] for x in result] == ["v1", "v1", leaf], result
    assert all(x["composition"] == composition for x in result), result


legacy = subprocess.check_output(
    [
        "kubectl",
        "exec",
        "-n",
        "envy-system",
        "deployment/postgres",
        "--",
        "psql",
        "-U",
        "envy",
        "-d",
        "envy",
        "-Atc",
        "SELECT to_regclass('installation_profile') IS NULL",
    ],
    text=True,
).strip()
assert legacy == "t", "upgrade fixture must begin without provider-binding metadata"
c = request(
    "/v1/compositions",
    "POST",
    {
        "project": "demo",
        "baseline": "staging",
        "name": "upgrade-smoke",
        "overrides": {"service-b": {"image": "envy/service-b:v2"}},
        "ttl": "20m",
    },
)
id = c["id"]
before = wait(id, "ready")
endpoint = before["endpoints"]["public"]["url"]
ns = "envy-" + id
inventory = subprocess.check_output(
    [
        "kubectl",
        "get",
        "deployment/service-b",
        "service/service-b",
        "-n",
        ns,
        "-o",
        "json",
    ],
    text=True,
)
uids = {x["kind"]: x["metadata"]["uid"] for x in json.loads(inventory)["items"]}
traffic(endpoint, id, "v2")
subprocess.run(
    [
        "helm",
        "upgrade",
        "envy",
        str(root / "deploy/helm/envy"),
        "-n",
        "envy-system",
        "-f",
        str(state / "values.json"),
        "--wait",
        "--wait-for-jobs",
        "--timeout",
        "5m",
    ],
    check=True,
)
after = wait(id, "ready")
assert after["endpoints"]["public"]["url"] == endpoint
inventory = json.loads(
    subprocess.check_output(
        [
            "kubectl",
            "get",
            "deployment/service-b",
            "service/service-b",
            "-n",
            ns,
            "-o",
            "json",
        ],
        text=True,
    )
)
assert {
    x["kind"]: x["metadata"]["uid"] for x in inventory["items"]
} == uids, "upgrade replaced workload identity"
traffic(endpoint, id, "v2")
traffic(
    json.loads((state / "catalog.json").read_text())["baseline"]["endpoint"], "", "v1"
)
binding = subprocess.check_output(
    [
        "kubectl",
        "exec",
        "-n",
        "envy-system",
        "deployment/postgres",
        "--",
        "psql",
        "-U",
        "envy",
        "-d",
        "envy",
        "-Atc",
        "SELECT provider FROM installation_profile",
    ],
    text=True,
).strip()
assert binding == "istio", binding
policy_mode = subprocess.check_output(
    [
        "kubectl",
        "exec",
        "-n",
        "envy-system",
        "deployment/postgres",
        "--",
        "psql",
        "-U",
        "envy",
        "-d",
        "envy",
        "-Atc",
        "SELECT namespace_policy_mode FROM installation_profile",
    ],
    text=True,
).strip()
assert (
    policy_mode == "legacy"
), f"upgrade unexpectedly changed preview connectivity: {policy_mode}"
request("/v1/compositions/" + id, "DELETE")
wait(id, "destroyed")
(state / "upgrade-result.json").write_text(
    json.dumps(
        {
            "from": json.loads((root / "deploy/testing/versions.json").read_text())[
                "istio_upgrade_from"
            ],
            "provider": binding,
            "namespace_policy_mode": policy_mode,
            "endpoint_preserved": True,
            "workload_uids_preserved": True,
            "https_verified": endpoint.startswith("https:"),
            "destroyed": True,
        },
        indent=2,
    )
    + "\n"
)
print(
    "Istio upgrade passed: legacy provider binding, live HTTPS traffic, stable workload identity and destruction"
)
