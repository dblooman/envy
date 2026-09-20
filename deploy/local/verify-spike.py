#!/usr/bin/env python3
"""Probe actual Envoy ingress; no service-to-service routing is simulated."""

import http.client
import json
import os
from pathlib import Path
import sys
import time

state = Path(os.environ["ENVY_STATE_DIR"])
port = int(os.environ["ENVY_PREVIEW_PORT"])
stage = sys.argv[1]


def call(host, baggage=()):
    c = http.client.HTTPConnection("127.0.0.1", port, timeout=8)
    try:
        c.putrequest("GET", "/", skip_host=True)
        c.putheader("Host", host)
        for value in baggage:
            c.putheader("baggage", value)
        c.endheaders()
        r = c.getresponse()
        body = r.read()
        return r.status, json.loads(body) if r.status == 200 else body.decode(
            errors="replace"
        )
    finally:
        c.close()


def chain(host, version, composition, baggage=()):
    status, result = call(host, baggage)
    assert status == 200, (host, status, result)
    hops = result["chain"]
    assert [h["service"] for h in hops] == ["gateway", "service-a", "service-b"], result
    assert [h["version"] for h in hops] == ["v1", "v1", version], result
    assert all(
        h["composition"] == composition and h["workload_id"] for h in hops
    ), result
    assert [h["deployment_composition"] for h in hops] == [
        "baseline",
        "baseline",
        composition or "baseline",
    ], result
    return result


def eventually(check, timeout=90):
    end = time.monotonic() + timeout
    while True:
        try:
            return check()
        except (AssertionError, OSError, ValueError) as e:
            if time.monotonic() >= end:
                raise AssertionError(f"{stage} deadline: {e}") from e
            time.sleep(0.5)


def baseline():
    value = chain(
        "baseline.envy.localhost",
        "v1",
        "",
        ("composition=spike", "composition=hostile,tenant=outside"),
    )
    if stage != "before":
        assert value == json.loads((state / "spike-baseline.json").read_text()), value
    return value


def absent():
    status, result = call("cmp-spike.envy.localhost")
    assert status == 404, (status, result)
    baseline()


if stage == "before":
    result = eventually(baseline)
    (state / "spike-baseline.json").write_text(json.dumps(result, indent=2) + "\n")
elif stage == "active":

    def active():
        value = chain(
            "cmp-spike.envy.localhost",
            "v2",
            "spike",
            ("composition=hostile", "tenant=outside,composition=another"),
        )
        base = baseline()
        assert [h["workload_id"] for h in value["chain"][:2]] == [
            h["workload_id"] for h in base["chain"][:2]
        ], value
        assert (
            value["chain"][2]["workload_id"] != base["chain"][2]["workload_id"]
        ), value
        assert call("unknown.envy.localhost")[0] == 404
        return value

    result = eventually(active)
    for _ in range(20):
        active()
    (state / "spike-composition.json").write_text(json.dumps(result, indent=2) + "\n")
elif stage in ("unpublished", "after"):
    eventually(absent)
else:
    raise SystemExit("unknown spike stage")
print(f"{stage}: passed")
