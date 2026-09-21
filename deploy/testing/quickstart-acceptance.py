#!/usr/bin/env python3
"""Test an installed quickstart via its forwarded gateway; never installs anything.

Usage: python3 deploy/testing/quickstart-acceptance.py KUBECONFIG [LOCAL_PORT]
Run only against a disposable quickstart installation. Creates/destroys one preview.
"""

import base64
import http.cookiejar
import json
import subprocess
import sys
import time
import urllib.request

kubeconfig = sys.argv[1]
port = sys.argv[2] if len(sys.argv) > 2 else "8080"
secret = subprocess.check_output(
    [
        "kubectl",
        "--kubeconfig",
        kubeconfig,
        "-n",
        "envy-quickstart",
        "get",
        "secret",
        "envy-quickstart-bootstrap",
        "-o",
        "json",
    ],
    text=True,
)
token = base64.b64decode(json.loads(secret)["data"]["token"]).decode()
opener = urllib.request.build_opener(
    urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
)


def request(path, data=None, method=None, host="127.0.0.1:8080", headers=None):
    req = urllib.request.Request(
        f"http://127.0.0.1:{port}{path}",
        data=json.dumps(data).encode() if data is not None else None,
        method=method,
    )
    req.add_header("Host", host)
    req.add_header("Content-Type", "application/json")
    for key, value in (headers or {}).items():
        req.add_header(key, value)
    with opener.open(req, timeout=15) as response:
        return json.load(response)


config = request("/auth/config")
request(
    "/auth/password",
    {"username": "admin", "password": "admin"},
    headers={"Origin": "http://127.0.0.1:8080", "X-CSRF-Token": config["csrf_token"]},
)
print("Password login passed", flush=True)
assert request("/products", host="shop.envy.localhost:8080")["price_minor"] == 1200
auth = {"Authorization": "Bearer " + token}
composition = request(
    "/v1/compositions",
    {
        "project": "shop",
        "baseline": "staging",
        "name": "quickstart-acceptance",
        "overrides": {"pricing": {"image": "davey/envy-demo:0.6.0-v2"}},
    },
    headers=auth,
)
identifier = composition["id"]
print("Created preview", identifier, flush=True)
try:
    deadline = time.monotonic() + 180
    while time.monotonic() < deadline:
        composition = request("/v1/compositions/" + identifier, headers=auth)
        if composition.get("phase") == "ready":
            break
        assert composition.get("phase") != "failed", composition
        time.sleep(3)
    else:
        raise AssertionError(composition)
    preview = request("/products", host="cmp-" + identifier + ".envy.localhost:8080")
    assert preview["price_minor"] == 990, preview
    assert request("/products", host="shop.envy.localhost:8080")["price_minor"] == 1200
    print("Preview price 990; shared baseline still 1200", flush=True)
finally:
    request("/v1/compositions/" + identifier, method="DELETE", headers=auth)
    print("Preview destruction requested", flush=True)
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        composition = request("/v1/compositions/" + identifier, headers=auth)
        if composition.get("phase") == "destroyed":
            print("Preview destroyed", flush=True)
            break
        time.sleep(3)
    else:
        raise AssertionError(composition)
