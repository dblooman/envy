#!/usr/bin/env python3
"""Read-only onboarding evidence; never registers, approves, or creates resources."""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request

GATES = (
    "application_effects",
    "identity",
    "dependencies",
    "schema",
    "image",
    "business_scenario",
)
ID = re.compile(r"[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?\Z")
MAX_BYTES = 2 * 1024 * 1024


class APIError(Exception):
    def __init__(self, status):
        self.status = status
        super().__init__(f"Envy HTTP {status}; inspect validation/discovery in Envy")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        fp.close()
        raise APIError(code)


class API:
    def __init__(self, url, token_file=None):
        parsed = urllib.parse.urlsplit(url)
        if (
            parsed.scheme not in ("http", "https")
            or not parsed.hostname
            or parsed.username is not None
            or parsed.password is not None
            or parsed.query
            or parsed.fragment
            or parsed.path not in ("", "/")
            or (
                parsed.scheme == "http"
                and parsed.hostname not in ("localhost", "127.0.0.1", "::1")
            )
        ):
            raise ValueError("API URL must be an HTTPS origin or loopback HTTP origin")
        self.url = url.rstrip("/")
        self.token = Path(token_file).read_text().strip() if token_file else ""
        if token_file and (
            not self.token
            or any(
                char.isspace() or ord(char) < 33 or ord(char) > 126
                for char in self.token
            )
        ):
            raise ValueError("Token file must contain one nonempty ASCII bearer token")
        self.opener = urllib.request.build_opener(NoRedirect())

    def call(self, method, path, body=None):
        # Enforce the read-only boundary even if this helper is later extended.
        allowed = (method == "GET" and path.startswith("/v1/projects/")) or (
            method == "POST"
            and (
                path == "/v1/catalog/validate"
                or re.fullmatch(
                    r"/v1/projects/[a-z0-9-]+/baselines/[a-z0-9-]+/components/[a-z0-9-]+/preview-profile/discover",
                    path,
                )
            )
        )
        if not allowed:
            raise ValueError("readiness helper refuses a mutating endpoint")
        headers = {"Content-Type": "application/json"}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        req = urllib.request.Request(
            self.url + path,
            method=method,
            headers=headers,
            data=None if body is None else json.dumps(body).encode(),
        )
        try:
            with self.opener.open(req, timeout=30) as response:
                data = response.read(MAX_BYTES + 1)
                if len(data) > MAX_BYTES:
                    raise ValueError("Envy response exceeds readiness report limit")
                return json.loads(data)
        except urllib.error.HTTPError as exc:
            exc.close()
            raise APIError(exc.code) from None
        except urllib.error.URLError:
            raise ValueError(
                "Cannot reach Envy API; check endpoint, TLS and connectivity"
            ) from None


def load_config(path):
    config = json.loads(path.read_text())
    if (
        set(config) != {"api_version", "catalog", "components", "prerequisites"}
        or config["api_version"] != "envy-onboarding/v1"
    ):
        raise ValueError(
            "Expected envy-onboarding/v1 with catalog, components and prerequisites"
        )
    catalog = json.loads((path.parent / config["catalog"]).read_text())
    project, baseline = catalog["project"]["id"], catalog["baseline"]["id"]
    if not all(isinstance(x, str) and ID.fullmatch(x) for x in (project, baseline)):
        raise ValueError("Invalid catalog project/baseline IDs")
    selected = config["components"]
    if (
        not isinstance(selected, list)
        or not 1 <= len(selected) <= 3
        or not all(isinstance(x, str) and ID.fullmatch(x) for x in selected)
        or len(set(selected)) != len(selected)
    ):
        raise ValueError("Select one to three unique catalog component IDs")
    profiles = {c["id"]: c for c in catalog["components"]}
    for name in selected:
        if (
            name not in profiles
            or name not in catalog["baseline"]["components"]
            or profiles[name].get("overridable") is not True
        ):
            raise ValueError("Selected component must be bound and overridable")
        if profiles[name]["profile"] not in (
            "http-small",
            "deployment",
            "deployment-composite",
        ):
            raise ValueError("Unsupported selected profile")
    gates = config["prerequisites"]
    if not isinstance(gates, dict) or set(gates) != set(GATES):
        raise ValueError("Prerequisites must include: " + ", ".join(GATES))
    for name, gate in gates.items():
        if not isinstance(gate, dict) or set(gate) != {"status", "owner", "evidence"}:
            raise ValueError(f"{name} requires status, owner and evidence")
        if gate["status"] not in ("pending", "confirmed", "not-applicable"):
            raise ValueError(f"Invalid prerequisite status: {name}")
        if not isinstance(gate["owner"], str) or not gate["owner"].strip():
            raise ValueError(f"{name} requires an owner")
        if not isinstance(gate["evidence"], str) or len(gate["evidence"]) > 2048:
            raise ValueError(f"{name} evidence must be text of at most 2048 characters")
        if gate["status"] != "pending" and not gate["evidence"].strip():
            raise ValueError(
                f"{name} requires evidence or a not-applicable explanation"
            )
    return config, catalog


def registered_baseline(api, project, baseline):
    cursor = ""
    seen = set()
    for _ in range(100):
        page = api.call(
            "GET",
            f"/v1/projects/{project}/baselines?"
            + urllib.parse.urlencode({"limit": 100, "after": cursor}),
        )
        for item in page["items"]:
            if item["id"] == baseline:
                return item
        cursor = page.get("next_cursor", "")
        if not cursor:
            return None
        if cursor in seen:
            raise ValueError("Repeated baseline pagination cursor")
        seen.add(cursor)
    raise ValueError("Baseline listing exceeded 100 pages")


def inspect_component(api, project, baseline, component):
    name = component["id"]
    if component["profile"] == "http-small":
        return {
            "component": name,
            "status": "manual-profile",
            "approval_required": False,
        }, True
    path = (
        f"/v1/projects/{project}/baselines/{baseline}/components/{name}/preview-profile"
    )
    approved = None
    try:
        approved = api.call("GET", path)
    except APIError as exc:
        if exc.status != 404:
            raise
    # Match creation semantics: discover with the saved approval's transformations.
    report = api.call(
        "POST", path + "/discover", approved["selection"] if approved else {}
    )
    current = bool(
        approved
        and approved.get("revision", 0) > 0
        and bool(approved.get("source_uid"))
        and bool(approved.get("contract"))
        and approved.get("source_uid") == report["source"]["uid"]
        and approved.get("contract") == report["contract"]
    )
    blockers = report["blockers"]
    if not isinstance(blockers, list):
        raise ValueError("Discovery returned invalid blockers")
    status = "blocked" if blockers else "approved" if current else "approval-required"
    result = {
        "component": name,
        "status": status,
        "approval_required": True,
        "source": report["source"],
        "blockers": blockers,
        "warnings": report.get("warnings", []),
        "source_read_rules": report.get("source_read_rules", []),
        "policy_scope": report.get("composite_policy_key", ""),
    }
    if current and not blockers:
        result["expected_preview_revision"] = approved["revision"]
    return result, current and not blockers


def assess(api, config, catalog):
    project, baseline = catalog["project"]["id"], catalog["baseline"]["id"]
    out = {
        "api_version": "envy-onboarding-report/v1",
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "project": project,
        "baseline": baseline,
        "api_origin": getattr(api, "url", None),
        "catalog_sha256": hashlib.sha256(
            json.dumps(catalog, sort_keys=True).encode()
        ).hexdigest(),
        "ready_for_creation_review": False,
        "checks": [],
        "components": [],
        "operator_prerequisites": config["prerequisites"],
        "limitations": [
            "Point-in-time read-only report; creation revalidates source and policy.",
            "Operator evidence is not verified cloud access, side-effect isolation or business correctness.",
            "No catalog registration, approval, workload creation, IAM mutation or application smoke command was performed; catalog validation can probe configured ingress paths.",
        ],
    }
    try:
        validated = api.call("POST", "/v1/catalog/validate", catalog)
        checks = validated["checks"]
        if not checks or not all(check.get("status") is True for check in checks):
            raise ValueError("Catalog validation did not pass all checks")
        out["checks"] = checks
        out["warnings"] = validated.get("warnings", [])
        saved = registered_baseline(api, project, baseline)
        if saved is None:
            out["checks"].append(
                {
                    "type": "RegisteredBaseline",
                    "status": False,
                    "message": "Register the validated catalog explicitly, then rerun readiness",
                }
            )
            return out
        normalized = validated["configuration"]
        if saved != normalized["baseline"]:
            raise ValueError(
                "Registered baseline differs from the validated catalog; refresh input"
            )
        for component in normalized["components"]:
            saved_component = api.call(
                "GET", f"/v1/projects/{project}/components/{component['id']}"
            )
            if saved_component != component:
                raise ValueError(
                    "Registered component differs from the validated catalog; refresh input"
                )
        out["checks"].append({"type": "RegisteredBaseline", "status": True})
    except (APIError, ValueError) as exc:
        out["checks"].append(
            {"type": "CatalogReadiness", "status": False, "message": str(exc)}
        )
        return out
    profiles = {c["id"]: c for c in catalog["components"]}
    ready = True
    for name in config["components"]:
        try:
            result, passed = inspect_component(api, project, baseline, profiles[name])
        except (APIError, ValueError) as exc:
            result, passed = (
                {"component": name, "status": "blocked", "message": str(exc)},
                False,
            )
        out["components"].append(result)
        ready = ready and passed
    out["ready_for_creation_review"] = ready and all(
        gate["status"] != "pending" for gate in config["prerequisites"].values()
    )
    return out


def write_report(path, report):
    path = Path(path)
    fd, temporary = tempfile.mkstemp(prefix=".readiness-", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as output:
            json.dump(report, output, indent=2)
            output.write("\n")
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, type=Path)
    parser.add_argument("--api-url", default=os.environ.get("ENVY_API_URL"))
    parser.add_argument("--token-file", default=os.environ.get("ENVY_API_TOKEN_FILE"))
    parser.add_argument("--output", type=Path)
    parser.add_argument(
        "--check-config",
        action="store_true",
        help="Validate local input only; no readiness claim",
    )
    args = parser.parse_args()
    try:
        config, catalog = load_config(args.config)
    except ValueError as exc:
        print(f"Readiness configuration invalid: {exc}", file=sys.stderr)
        return 1
    except (KeyError, TypeError, OSError):
        print(
            "Cannot load readiness configuration/catalog; check paths and required fields",
            file=sys.stderr,
        )
        return 1
    try:
        if args.check_config:
            print("Configuration valid; no API or prerequisite checks performed")
            return 0
        if not args.api_url:
            raise ValueError("--api-url or ENVY_API_URL is required")
        if args.output and args.output.resolve() in (
            args.config.resolve(),
            (args.config.parent / config["catalog"]).resolve(),
            Path(args.token_file).resolve() if args.token_file else None,
        ):
            raise ValueError(
                "Output must not overwrite configuration, catalog or token file"
            )
        report = assess(API(args.api_url, args.token_file), config, catalog)
        if args.output:
            write_report(args.output, report)
        else:
            print(json.dumps(report, indent=2))
        return 0 if report["ready_for_creation_review"] else 2
    except (ValueError, KeyError, TypeError, OSError) as exc:
        print(
            f"Readiness input or transport error: {type(exc).__name__}; check configuration and API access",
            file=sys.stderr,
        )
        return 1


if __name__ == "__main__":
    sys.exit(main())
