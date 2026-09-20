#!/usr/bin/env python3
"""REST-only LAN acceptance. No SSH, kubectl, Docker or DNS dependency."""

import argparse
import json
from pathlib import Path
import time
import urllib.error
import urllib.request
from urllib.parse import urlsplit
import uuid

from render import load_builds


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Client:
    def __init__(self, api, ingress, token):
        for endpoint in (api, ingress):
            url = urlsplit(endpoint)
            if (
                url.scheme not in ("http", "https")
                or not url.hostname
                or url.username
                or url.query
                or url.fragment
                or url.path not in ("", "/")
            ):
                raise ValueError("API and ingress must be plain HTTP(S) origins")
        self.api, self.ingress, self.token = api.rstrip("/"), ingress.rstrip("/"), token
        self.http = urllib.request.build_opener(
            urllib.request.ProxyHandler({}), NoRedirect()
        )

    def call(self, origin, method, path, headers=None, body=None):
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(
            origin + path, data=data, headers=headers or {}, method=method
        )
        if data is not None:
            request.add_header("Content-Type", "application/json")
        try:
            response = self.http.open(request, timeout=10)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read(65537)
            if len(raw) > 65536:
                raise RuntimeError("response exceeds acceptance limit")
            try:
                body = json.loads(raw)
            except ValueError:
                body = None
            return (
                response.status,
                dict((k.lower(), v) for k, v in response.headers.items()),
                body,
            )

    def api_call(self, method, path, body=None, key=None, expected=200):
        headers = {"Authorization": "Bearer " + self.token, "X-Envy-Channel": "api"}
        if key:
            headers["Idempotency-Key"] = key
        status, _, result = self.call(self.api, method, path, headers, body)
        if status != expected:
            raise RuntimeError(f"{method} {path}: HTTP {status}, expected {expected}")
        return result

    def traffic(self, host):
        return self.call(
            self.ingress,
            "GET",
            "/products",
            {"Host": host, "baggage": "composition=hostile,tenant=test"},
        )


def check_chain(observation, composition, price):
    status, headers, body = observation
    if status != 200 or body is None or body.get("price_minor") != price:
        raise RuntimeError(f"incorrect business response: HTTP {status}, body={body}")
    identities = []
    for component in ("storefront", "pricing"):
        if headers.get(f"x-shop-{component}-context", "") != composition:
            raise RuntimeError(f"incorrect propagated context at {component}")
        identity = headers.get(f"x-shop-{component}-workload", "")
        if not identity:
            raise RuntimeError(f"missing workload identity at {component}")
        identities.append(identity)
    return identities


def accept(client, builds, report, timeout=180):
    baseline_price = builds["expected"]["baseline_price_minor"]
    branch_price = builds["expected"]["branch_price_minor"]
    session = client.api_call("GET", "/v1/session")
    actor = session.get("principal", {})
    if actor.get("kind") != "service" or not actor.get("id"):
        raise RuntimeError("LAN acceptance requires a named machine credential")
    report["actor"] = {"kind": actor["kind"], "id": actor["id"]}
    status, _, _ = client.call(
        client.api, "GET", "/v1/session", {"Authorization": "Bearer invalid"}
    )
    if status != 401:
        raise RuntimeError("invalid API credentials were accepted")
    baseline_host = "baseline.envy.test:30080"
    baseline_observation = client.traffic(baseline_host)
    original = check_chain(baseline_observation, "", baseline_price)
    report["baseline"] = {
        "body": baseline_observation[2],
        "storefront_workload": original[0],
        "pricing_workload": original[1],
    }
    if client.traffic("unknown.envy.test:30080")[0] != 404:
        raise RuntimeError("unknown ingress hostname forwards")
    composition = None
    destroyed = False
    try:
        key = "lan-" + str(uuid.uuid4())
        intent = {
            "project": "lan",
            "baseline": "staging",
            "name": "branch-pricing",
            "ttl": "10m",
            "overrides": {"pricing": {"image": builds["pricing_branch"]["image"]}},
        }
        composition = client.api_call("POST", "/v1/compositions", intent, key, 202)
        report["composition"] = composition["id"]
        replay = client.api_call("POST", "/v1/compositions", intent, key, 202)
        if replay["id"] != composition["id"]:
            raise RuntimeError("idempotent retry created another composition")
        path = "/v1/compositions/" + composition["id"]

        def baseline():
            if (
                check_chain(client.traffic(baseline_host), "", baseline_price)
                != original
            ):
                raise RuntimeError("baseline workload identities changed")

        def wait(phase):
            deadline = time.monotonic() + timeout
            current = None
            while time.monotonic() < deadline:
                current = client.api_call("GET", path)
                baseline()
                if current["phase"] == phase:
                    return current
                if current["phase"] == "failed" and phase != "destroyed":
                    raise RuntimeError(
                        "composition failed; inspect its status through Envy"
                    )
                time.sleep(1)
            report["last_phase"] = current.get("phase") if current else None
            raise RuntimeError(f"timed out waiting for {phase}")

        ready = wait("ready")
        endpoint = ready["endpoints"]["public"]
        url = urlsplit(endpoint["url"])
        if (
            not endpoint["ready"]
            or url.hostname != "cmp-" + composition["id"] + ".envy.test"
            or url.port != 30080
        ):
            raise RuntimeError("unexpected composition endpoint")
        observations = []
        for _ in range(5):
            observation = client.traffic(url.netloc)
            preview = check_chain(observation, composition["id"], branch_price)
            if preview[0] != original[0] or preview[1] == original[1]:
                raise RuntimeError("incorrect inherited/override workload selection")
            baseline()
            observations.append(
                {
                    "body": observation[2],
                    "storefront_workload": preview[0],
                    "pricing_workload": preview[1],
                    "storefront_context": observation[1].get(
                        "x-shop-storefront-context"
                    ),
                    "pricing_context": observation[1].get("x-shop-pricing-context"),
                }
            )
        report["observations"] = observations
        report["verification_level"] = ready.get("verification_level")
        client.api_call("DELETE", path, expected=202)
        wait("destroyed")
        status, headers, _ = client.traffic(url.netloc)
        if status != 404 or headers.get("x-envy-route"):
            raise RuntimeError("destroyed hostname still has a preview route")
        baseline()
        destroyed = True
        report["result"] = "passed"
        report["cluster_checks"] = (
            "pending: inspect owned namespace absence and retained PostgreSQL on the cluster Mac"
        )
    finally:
        if composition and not destroyed:
            try:
                client.api_call(
                    "DELETE", "/v1/compositions/" + composition["id"], expected=202
                )
                report["cleanup"] = "deletion requested; inspect until destroyed"
            except Exception:
                report["cleanup"] = (
                    "deletion request failed; inspect composition before TTL expires"
                )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--api", default="http://192.168.1.172:30081")
    parser.add_argument("--ingress", default="http://192.168.1.172:30080")
    parser.add_argument("--token-file", required=True)
    parser.add_argument("--builds", default="deploy/lan/builds.json")
    parser.add_argument("--report", default=".envy/lan/acceptance.json")
    args = parser.parse_args()
    builds = load_builds(args.builds)
    report = {
        "result": "failed",
        "api": args.api,
        "ingress": args.ingress,
        "builds": builds,
        "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    start = time.monotonic()
    try:
        accept(
            Client(args.api, args.ingress, Path(args.token_file).read_text().strip()),
            builds,
            report,
        )
    except Exception as error:
        report["error"] = str(error)
        raise
    finally:
        report["duration_seconds"] = round(time.monotonic() - start, 3)
        output = Path(args.report)
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(report, indent=2) + "\n")
        print(f"Acceptance report: {output}")


if __name__ == "__main__":
    main()
