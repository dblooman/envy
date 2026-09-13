#!/usr/bin/env python3
"""Exercise a remote development composition through the same REST API as agents."""
import json
import os
from pathlib import Path
import time
import urllib.error
import urllib.request
from urllib.parse import urlsplit
import uuid


def main():
    api = os.environ["ENVY_API_URL"].rstrip("/")
    token = Path(os.environ["ENVY_API_TOKEN_FILE"]).read_text().strip()
    preview_port = int(os.environ.get("ENVY_REMOTE_PREVIEW_PORT", "18080"))
    # Bypass desktop proxy settings for explicitly loopback-forwarded traffic.
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def request(method, path, body=None, key=None):
        headers = {"Authorization": "Bearer " + token, "X-Envy-Channel": "api"}
        if key:
            headers["Idempotency-Key"] = key
        if body is not None:
            body = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        with client.open(urllib.request.Request(api + path, data=body, headers=headers, method=method), timeout=15) as response:
            return json.load(response)

    def traffic(endpoint, composition, version):
        parsed = urlsplit(endpoint)
        if parsed.scheme != "http" or parsed.port != preview_port or not parsed.hostname.endswith(".envy.localhost"):
            raise RuntimeError("returned preview URL does not match the remote development tunnel")
        req = urllib.request.Request(f"http://127.0.0.1:{preview_port}/", headers={
            "Host": parsed.netloc, "baggage": "composition=hostile"})
        with client.open(req, timeout=15) as response:
            chain = json.load(response)["chain"]
        if len(chain) != 3:
            raise RuntimeError("expected three real service responses")
        for hop, service, expected in zip(chain, ["gateway", "service-a", "service-b"], ["v1", "v1", version]):
            if hop["service"] != service or hop["version"] != expected or hop["composition"] != composition or not hop["workload_id"]:
                raise RuntimeError(f"unexpected service response: {hop}")
        return [hop["workload_id"] for hop in chain]

    baseline_url = f"http://baseline.envy.localhost:{preview_port}"
    request("GET", "/v1/session")
    original = traffic(baseline_url, "", "v1")
    composition = None
    destroyed = False
    try:
        key = "remote-smoke-" + str(uuid.uuid4())
        intent = {"project": "demo", "baseline": "staging", "name": "remote-smoke", "ttl": "10m", "overrides": {"service-b": {"image": "envy/service-b:v2"}}}
        composition = request("POST", "/v1/compositions", intent, key)
        replay = request("POST", "/v1/compositions", intent, key)
        if replay["id"] != composition["id"]:
            raise RuntimeError("idempotent retry created a duplicate")
        path = "/v1/compositions/" + composition["id"]

        def wait(phase):
            deadline = time.monotonic() + 180
            while time.monotonic() < deadline:
                current = request("GET", path)
                if traffic(baseline_url, "", "v1") != original:
                    raise RuntimeError("baseline workload identities changed")
                if current["phase"] == phase:
                    return current
                if current["phase"] == "failed" and phase != "destroyed":
                    raise RuntimeError(f"composition failed: {current.get('last_error')}")
                time.sleep(1)
            raise RuntimeError(f"timed out waiting for {phase}")

        composition = wait("ready")
        endpoint = composition["endpoints"]["public"]["url"]
        if not composition["endpoints"]["public"]["ready"]:
            raise RuntimeError("ready composition has an unready endpoint")
        preview = traffic(endpoint, composition["id"], "v2")
        if preview[:2] != original[:2] or preview[2] == original[2]:
            raise RuntimeError("incorrect inherited/override workload identities")
        request("PATCH", path, {"expected_generation": composition["generation"], "overrides": {"service-b": {"image": "envy/service-b:v3"}}})
        composition = wait("ready")
        if composition["endpoints"]["public"]["url"] != endpoint:
            raise RuntimeError("update changed the composition URL")
        traffic(endpoint, composition["id"], "v3")
        request("DELETE", path)
        wait("destroyed")
        destroyed = True
        try:
            traffic(endpoint, composition["id"], "v3")
        except urllib.error.HTTPError as error:
            if error.code != 404:
                raise
        else:
            raise RuntimeError("destroyed endpoint still forwarded")
        print(json.dumps({"result": "passed", "composition": composition["id"], "checks": ["remote API", "idempotency", "preview v2", "stable baseline", "update v3", "destroyed URL 404"]}))
    finally:
        if composition and not destroyed:
            try:
                request("DELETE", "/v1/compositions/" + composition["id"])
            except Exception:
                print("Cleanup request failed; inspect composition " + composition["id"], file=__import__("sys").stderr)


if __name__ == "__main__":
    main()
