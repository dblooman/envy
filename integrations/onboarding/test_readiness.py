import copy
from contextlib import contextmanager, redirect_stderr, redirect_stdout
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import threading
import unittest
from unittest.mock import Mock, patch
import urllib.parse


spec = importlib.util.spec_from_file_location(
    "onboarding_readiness", Path(__file__).with_name("readiness.py")
)
readiness = importlib.util.module_from_spec(spec)
spec.loader.exec_module(readiness)


def inputs(profile="deployment"):
    catalog = {
        "project": {"id": "shop", "name": "Shop"},
        "components": [
            {
                "id": "api",
                "project": "shop",
                "profile": profile,
                "port": 8080,
                "overridable": True,
            }
        ],
        "baseline": {
            "id": "staging",
            "project": "shop",
            "components": {
                "api": {"service_host": "api.staging.svc.cluster.local", "port": 8080}
            },
        },
    }
    config = {
        "api_version": "envy-onboarding/v1",
        "catalog": "catalog.json",
        "components": ["api"],
        "prerequisites": {
            name: {
                "status": "confirmed",
                "owner": "operator",
                "evidence": "Synthetic test evidence",
            }
            for name in readiness.GATES
        },
    }
    return config, catalog


class FakeAPI:
    def __init__(self, catalog):
        self.calls = []
        self.catalog = copy.deepcopy(catalog)
        self.validated = {
            "checks": [{"type": "Catalog", "status": True}],
            "warnings": [],
            "configuration": copy.deepcopy(catalog),
        }
        self.baseline_pages = [{"items": [copy.deepcopy(catalog["baseline"])]}]
        self.page = 0
        self.approval = {
            "revision": 3,
            "source_uid": "source-uid",
            "contract": "approved-contract",
            "selection": {
                "deployment": "api-deployment",
                "container": "application",
                "env": {
                    "DEPENDENCY_URL": "http://dependency.staging.svc.cluster.local"
                },
                "config_map_keys": {"settings": {"feature": "fixture"}},
            },
        }
        self.discovery = {
            "source": {"uid": "source-uid", "namespace": "staging"},
            "contract": "approved-contract",
            "blockers": [],
            "warnings": ["Shared dependencies need operator evidence"],
            "source_read_rules": [{"resources": ["configmaps"], "verbs": ["get"]}],
        }

    @staticmethod
    def result(value):
        if isinstance(value, Exception):
            raise value
        return copy.deepcopy(value)

    def call(self, method, path, body=None):
        self.calls.append((method, path, copy.deepcopy(body)))
        if (method, path) == ("POST", "/v1/catalog/validate"):
            return self.result(self.validated)
        if method == "GET" and path.startswith("/v1/projects/shop/baselines?"):
            page = self.baseline_pages[min(self.page, len(self.baseline_pages) - 1)]
            self.page += 1
            return self.result(page)
        if (method, path) == ("GET", "/v1/projects/shop/components/api"):
            return self.result(self.catalog["components"][0])
        if path == "/v1/projects/shop/baselines/staging/components/api/preview-profile":
            if method != "GET":
                raise AssertionError("Readiness attempted to approve a preview")
            return self.result(self.approval)
        if (method, path) == (
            "POST",
            "/v1/projects/shop/baselines/staging/components/api/preview-profile/discover",
        ):
            return self.result(self.discovery)
        raise AssertionError(f"Unexpected readiness request: {method} {path}")


class ReadinessEvidenceTests(unittest.TestCase):
    def test_ready_requires_current_approval_and_uses_saved_transformations(self):
        config, catalog = inputs()
        api = FakeAPI(catalog)
        report = readiness.assess(api, config, catalog)
        self.assertTrue(report["ready_for_creation_review"])
        self.assertEqual(report["components"][0]["expected_preview_revision"], 3)
        discovery = [call for call in api.calls if call[1].endswith("/discover")]
        self.assertEqual(discovery[0][2], api.approval["selection"])
        self.assertEqual(
            report["components"][0]["source_read_rules"],
            api.discovery["source_read_rules"],
        )
        self.assertIn("point-in-time", " ".join(report["limitations"]).lower())

    def test_each_pending_operator_gate_prevents_readiness(self):
        for gate in readiness.GATES:
            with self.subTest(gate=gate):
                config, catalog = inputs()
                config["prerequisites"][gate]["status"] = "pending"
                report = readiness.assess(FakeAPI(catalog), config, catalog)
                self.assertFalse(report["ready_for_creation_review"])
                self.assertEqual(report["components"][0]["status"], "approved")

    def test_scoped_business_evidence_is_operator_supplied_not_http_proof(self):
        config, catalog = inputs("deployment-composite")
        gate = config["prerequisites"]["business_scenario"]
        gate.update(status="pending", evidence="")
        pending = readiness.assess(FakeAPI(catalog), config, catalog)
        self.assertFalse(pending["ready_for_creation_review"])

        gate.update(
            status="confirmed",
            evidence="shop/staging/api: synthetic order reached selected dependency, run 42",
        )
        supplied = readiness.assess(FakeAPI(catalog), config, catalog)
        self.assertTrue(supplied["ready_for_creation_review"])
        self.assertEqual(
            supplied["operator_prerequisites"]["business_scenario"]["evidence"],
            gate["evidence"],
        )
        self.assertTrue(
            any("not verified" in limitation for limitation in supplied["limitations"])
        )

    def test_stale_or_invalid_approval_does_not_supply_a_revision(self):
        for field, value in (
            ("source_uid", "recreated-source"),
            ("contract", "changed-execution"),
            ("revision", 0),
            ("revision", -1),
        ):
            with self.subTest(field=field, value=value):
                config, catalog = inputs()
                api = FakeAPI(catalog)
                api.approval[field] = value
                report = readiness.assess(api, config, catalog)
                self.assertFalse(report["ready_for_creation_review"])
                self.assertEqual(report["components"][0]["status"], "approval-required")
                self.assertNotIn("expected_preview_revision", report["components"][0])

    def test_missing_approval_still_discovers_and_surfaces_blockers(self):
        for blockers in ([], ["cannot read Secret preview-db"]):
            with self.subTest(blockers=blockers):
                config, catalog = inputs()
                api = FakeAPI(catalog)
                api.approval = readiness.APIError(404)
                api.discovery["blockers"] = blockers
                report = readiness.assess(api, config, catalog)
                component = report["components"][0]
                self.assertFalse(report["ready_for_creation_review"])
                self.assertEqual(
                    component["status"], "blocked" if blockers else "approval-required"
                )
                self.assertEqual(component["blockers"], blockers)
                self.assertNotIn("expected_preview_revision", component)
                self.assertEqual(
                    [call[2] for call in api.calls if call[1].endswith("/discover")],
                    [{}],
                )

    def test_blockers_override_a_matching_approval(self):
        config, catalog = inputs()
        api = FakeAPI(catalog)
        api.discovery["blockers"] = ["source Deployment must have a ready rollout"]
        report = readiness.assess(api, config, catalog)
        self.assertFalse(report["ready_for_creation_review"])
        self.assertEqual(report["components"][0]["status"], "blocked")
        self.assertNotIn("expected_preview_revision", report["components"][0])

    def test_catalog_failures_never_produce_readiness(self):
        for failure in (
            readiness.APIError(403),
            readiness.APIError(503),
            {"checks": []},
            {"checks": [{"status": False}]},
            {"checks": [{"status": "true"}]},
        ):
            with self.subTest(failure=repr(failure)):
                config, catalog = inputs()
                api = FakeAPI(catalog)
                api.validated = failure
                report = readiness.assess(api, config, catalog)
                self.assertFalse(report["ready_for_creation_review"])
                self.assertTrue(
                    any(check["status"] is False for check in report["checks"])
                )
                self.assertEqual(report["components"], [])
                self.assertEqual(len(api.calls), 1)

    def test_unregistered_baseline_cannot_pass_even_with_manual_profile(self):
        config, catalog = inputs("http-small")
        api = FakeAPI(catalog)
        api.baseline_pages = [{"items": []}]
        report = readiness.assess(api, config, catalog)
        self.assertFalse(report["ready_for_creation_review"])
        self.assertTrue(any(check["status"] is False for check in report["checks"]))
        self.assertFalse(any(call[1].endswith("/discover") for call in api.calls))

    def test_registered_catalog_drift_requires_explicit_update(self):
        for changed in ("baseline", "component"):
            with self.subTest(changed=changed):
                config, catalog = inputs("http-small")
                api = FakeAPI(catalog)
                if changed == "baseline":
                    api.baseline_pages[0]["items"][0]["components"]["api"]["port"] = (
                        9090
                    )
                else:
                    api.catalog["components"][0]["port"] = 9090
                report = readiness.assess(api, config, catalog)
                self.assertFalse(report["ready_for_creation_review"])
                self.assertTrue(
                    any(check["status"] is False for check in report["checks"])
                )

    def test_component_api_errors_fail_closed(self):
        for location, code in (
            ("approval", 403),
            ("approval", 500),
            ("discovery", 403),
        ):
            with self.subTest(location=location, code=code):
                config, catalog = inputs()
                api = FakeAPI(catalog)
                setattr(api, location, readiness.APIError(code))
                report = readiness.assess(api, config, catalog)
                self.assertFalse(report["ready_for_creation_review"])
                self.assertEqual(report["components"][0]["status"], "blocked")
                self.assertNotIn("expected_preview_revision", report["components"][0])

    def test_manual_profile_requires_catalog_and_operator_evidence(self):
        config, catalog = inputs("http-small")
        api = FakeAPI(catalog)
        report = readiness.assess(api, config, catalog)
        self.assertTrue(report["ready_for_creation_review"])
        self.assertEqual(report["components"][0]["status"], "manual-profile")
        self.assertFalse(any("preview-profile" in call[1] for call in api.calls))
        self.assertIn(("GET", "/v1/projects/shop/components/api", None), api.calls)


class PaginationTests(unittest.TestCase):
    def test_follows_encoded_cursor_and_returns_registered_baseline(self):
        _, catalog = inputs()
        api = FakeAPI(catalog)
        cursor = "opaque /+&token=next"
        api.baseline_pages = [
            {"items": [{"id": "different"}], "next_cursor": cursor},
            {"items": [catalog["baseline"]]},
        ]
        baseline = readiness.registered_baseline(api, "shop", "staging")
        self.assertEqual(baseline, catalog["baseline"])
        query = urllib.parse.parse_qs(urllib.parse.urlsplit(api.calls[1][1]).query)
        self.assertEqual(query["after"], [cursor])
        self.assertEqual(query["limit"], ["100"])

    def test_repeated_cursor_cannot_loop_forever(self):
        _, catalog = inputs()
        api = FakeAPI(catalog)
        api.baseline_pages = [{"items": [], "next_cursor": "repeat"}]
        with self.assertRaisesRegex(ValueError, "Repeated"):
            readiness.registered_baseline(api, "shop", "staging")
        self.assertEqual(len(api.calls), 2)

    def test_excessive_pagination_is_bounded(self):
        _, catalog = inputs()
        api = FakeAPI(catalog)
        api.baseline_pages = [{"items": [], "next_cursor": str(i)} for i in range(101)]
        with self.assertRaisesRegex(ValueError, "100 pages"):
            readiness.registered_baseline(api, "shop", "staging")
        self.assertEqual(len(api.calls), 100)


@contextmanager
def http_origin(response):
    received = []

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_GET(self):
            self.respond()

        def do_POST(self):
            self.respond()

        def respond(self):
            size = int(self.headers.get("Content-Length", "0"))
            received.append(
                (
                    self.command,
                    self.path,
                    self.headers.get("Authorization"),
                    self.rfile.read(size),
                )
            )
            status, headers, body = response()
            self.send_response(status)
            for name, value in headers.items():
                self.send_header(name, value)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            try:
                self.wfile.write(body)
            except (BrokenPipeError, ConnectionResetError):
                pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(
        target=server.serve_forever, kwargs={"poll_interval": 0.01}, daemon=True
    )
    thread.start()
    try:
        yield f"http://127.0.0.1:{server.server_port}", received
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)


class HTTPBoundaryTests(unittest.TestCase):
    def test_redirects_cannot_forward_bearer_token(self):
        for status in (301, 302, 303, 307, 308):
            with (
                self.subTest(status=status),
                tempfile.TemporaryDirectory() as temporary,
            ):
                token = Path(temporary) / "token"
                token.write_text("synthetic-test-token\n")
                with http_origin(lambda: (200, {}, b"{}")) as (target, forwarded):
                    with http_origin(
                        lambda: (status, {"Location": target + "/leak"}, b"")
                    ) as (origin, received):
                        api = readiness.API(origin, token)
                        with self.assertRaises(readiness.APIError) as error:
                            api.call("GET", "/v1/projects/shop/components/api")
                        self.assertEqual(error.exception.status, status)
                        self.assertEqual(received[0][2], "Bearer synthetic-test-token")
                        self.assertEqual(forwarded, [])

    def test_mutating_endpoints_are_refused_before_transport(self):
        api = readiness.API("http://127.0.0.1:1")
        api.opener = Mock()
        for method, path in (
            ("POST", "/v1/catalog/register"),
            ("POST", "/v1/compositions"),
            ("DELETE", "/v1/projects/shop"),
            ("PUT", "/v1/projects/shop/components/api"),
            (
                "POST",
                "/v1/projects/shop/baselines/staging/components/api/preview-profile",
            ),
            (
                "PATCH",
                "/v1/projects/shop/baselines/staging/components/api/preview-profile/discover",
            ),
            ("GET", "/v1/compositions"),
        ):
            with self.subTest(method=method, path=path):
                with self.assertRaisesRegex(ValueError, "mutating endpoint"):
                    api.call(method, path, {})
        api.opener.open.assert_not_called()

    def test_only_bounded_json_responses_are_accepted(self):
        for payload in (b"x" * (readiness.MAX_BYTES + 1), b"not-json"):
            with self.subTest(size=len(payload)):
                with http_origin(lambda: (200, {}, payload)) as (origin, _):
                    with self.assertRaises(ValueError):
                        readiness.API(origin).call(
                            "GET", "/v1/projects/shop/components/api"
                        )

    def test_api_error_does_not_reveal_response_body(self):
        with http_origin(lambda: (503, {}, b"private-error-details")) as (origin, _):
            with self.assertRaises(readiness.APIError) as error:
                readiness.API(origin).call("POST", "/v1/catalog/validate", {})
            self.assertEqual(error.exception.status, 503)
            self.assertNotIn("private-error-details", str(error.exception))

    def test_origin_restrictions(self):
        for url in (
            "http://external.example",
            "file:///tmp/api",
            "https://user:pass@example.test",
            "https://example.test/v1",
            "https://example.test?token=secret",
            "https://example.test#fragment",
            "https://",
        ):
            with self.subTest(url=url), self.assertRaises(ValueError):
                readiness.API(url)

    def test_invalid_tokens_are_rejected_without_disclosure_or_transport(self):
        for value in (
            "",
            "synthetic-secret\nsecond-line",
            "synthetic-secret\x00",
            "synthetic-secreté",
        ):
            with (
                self.subTest(value=repr(value)),
                tempfile.TemporaryDirectory() as temporary,
            ):
                token = Path(temporary) / "token"
                token.write_text(value)
                with patch.object(readiness.urllib.request, "build_opener") as opener:
                    with self.assertRaises(ValueError) as error:
                        readiness.API("https://example.test", token)
                    opener.assert_not_called()
                self.assertNotIn("synthetic-secret", str(error.exception))


class ConfigurationTests(unittest.TestCase):
    def load(self, config, catalog):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "catalog.json").write_text(json.dumps(catalog))
            path = root / "onboarding.json"
            path.write_text(json.dumps(config))
            return readiness.load_config(path)

    def test_pending_and_explained_not_applicable_are_valid_inputs(self):
        for status, evidence in (
            ("pending", ""),
            ("not-applicable", "No schema changes"),
        ):
            with self.subTest(status=status):
                config, catalog = inputs()
                config["prerequisites"]["schema"].update(
                    status=status, evidence=evidence
                )
                loaded, _ = self.load(config, catalog)
                self.assertEqual(loaded["prerequisites"]["schema"]["status"], status)

    def test_incomplete_or_invalid_operator_gates_are_rejected(self):
        for change in (
            lambda gates: gates.pop("identity"),
            lambda gates: gates.update(extra={}),
            lambda gates: gates["identity"].update(status="done"),
            lambda gates: gates["identity"].update(owner=" "),
            lambda gates: gates["identity"].update(evidence=""),
            lambda gates: gates["identity"].update(evidence="x" * 2049),
            lambda gates: gates["identity"].update(evidence=123),
            lambda gates: gates["identity"].update(extra="unexpected"),
        ):
            config, catalog = inputs()
            change(config["prerequisites"])
            with (
                self.subTest(prerequisites=config["prerequisites"]),
                self.assertRaises(ValueError),
            ):
                self.load(config, catalog)

    def test_component_selection_is_small_unique_bound_and_overridable(self):
        for selection in (
            [],
            ["api", "api"],
            ["a", "b", "c", "d"],
            ["API"],
            [None],
            ["missing"],
        ):
            config, catalog = inputs()
            config["components"] = selection
            with self.subTest(selection=selection), self.assertRaises(ValueError):
                self.load(config, catalog)
        for change in (
            lambda c: c["baseline"]["components"].clear(),
            lambda c: c["components"][0].update(overridable=False),
            lambda c: c["components"][0].update(profile="worker"),
            lambda c: c["project"].update(id="../other"),
        ):
            config, catalog = inputs()
            change(catalog)
            with self.subTest(catalog=catalog), self.assertRaises(ValueError):
                self.load(config, catalog)

    def test_schema_version_and_unknown_config_keys_are_rejected(self):
        for change in (
            lambda c: c.update(api_version="v2"),
            lambda c: c.update(auto_approve=True),
        ):
            config, catalog = inputs()
            change(config)
            with self.subTest(config=config), self.assertRaises(ValueError):
                self.load(config, catalog)

    def test_config_only_mode_never_contacts_api(self):
        config, catalog = inputs()
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "catalog.json").write_text(json.dumps(catalog))
            path = root / "onboarding.json"
            path.write_text(json.dumps(config))
            output = io.StringIO()
            with (
                patch.object(readiness, "API") as api,
                patch(
                    "sys.argv", ["readiness", "--config", str(path), "--check-config"]
                ),
                redirect_stdout(output),
            ):
                self.assertEqual(readiness.main(), 0)
            api.assert_not_called()
            self.assertIn("no API", output.getvalue())

    def test_config_only_mode_rejects_missing_or_invalid_gates(self):
        for change in (
            lambda gates: gates.pop("identity"),
            lambda gates: gates["identity"].update(status="unverified"),
        ):
            config, catalog = inputs()
            change(config["prerequisites"])
            with tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                (root / "catalog.json").write_text(json.dumps(catalog))
                path = root / "onboarding.json"
                path.write_text(json.dumps(config))
                with (
                    patch.object(readiness, "API") as api,
                    patch(
                        "sys.argv",
                        ["readiness", "--config", str(path), "--check-config"],
                    ),
                    redirect_stdout(io.StringIO()),
                    redirect_stderr(io.StringIO()) as error,
                ):
                    self.assertEqual(readiness.main(), 1)
                api.assert_not_called()
                self.assertIn("configuration", error.getvalue())

    def test_output_cannot_overwrite_inputs_token_or_their_symlinks(self):
        config, catalog = inputs()
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            catalog_path = root / "catalog.json"
            catalog_path.write_text(json.dumps(catalog))
            config_path = root / "onboarding.json"
            config_path.write_text(json.dumps(config))
            token_path = root / "token"
            token_path.write_text("synthetic-token")
            alias = root / "token-alias"
            alias.symlink_to(token_path)
            originals = {
                path: path.read_bytes()
                for path in (catalog_path, config_path, token_path)
            }
            for output_path in (*originals, alias):
                with self.subTest(output=output_path):
                    argv = [
                        "readiness",
                        "--config",
                        str(config_path),
                        "--api-url",
                        "https://example.test",
                        "--token-file",
                        str(token_path),
                        "--output",
                        str(output_path),
                    ]
                    with (
                        patch.object(readiness, "API") as api,
                        patch("sys.argv", argv),
                        redirect_stderr(io.StringIO()) as error,
                    ):
                        self.assertEqual(readiness.main(), 1)
                    api.assert_not_called()
                    self.assertNotIn("synthetic-token", error.getvalue())
                    for path, original in originals.items():
                        self.assertEqual(path.read_bytes(), original)

    def test_pending_cli_report_has_non_success_exit_and_private_permissions(self):
        config, catalog = inputs()
        config["prerequisites"]["identity"]["status"] = "pending"
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "catalog.json").write_text(json.dumps(catalog))
            config_path = root / "onboarding.json"
            config_path.write_text(json.dumps(config))
            output_path = root / "readiness.json"
            argv = [
                "readiness",
                "--config",
                str(config_path),
                "--api-url",
                "https://example.test",
                "--output",
                str(output_path),
            ]
            with (
                patch.object(readiness, "API", return_value=FakeAPI(catalog)),
                patch("sys.argv", argv),
            ):
                self.assertEqual(readiness.main(), 2)
            report = json.loads(output_path.read_text())
            self.assertFalse(report["ready_for_creation_review"])
            self.assertEqual(output_path.stat().st_mode & 0o077, 0)
            self.assertEqual(list(root.glob(".readiness-*")), [])


class MalformedEvidenceTests(unittest.TestCase):
    def test_empty_approval_identity_and_non_list_blockers_cannot_pass(self):
        for missing in ("source_uid", "contract", "blockers"):
            with self.subTest(missing=missing):
                config, catalog = inputs()
                api = FakeAPI(catalog)
                if missing == "blockers":
                    api.discovery["blockers"] = ""
                else:
                    api.approval[missing] = ""
                    if missing == "source_uid":
                        api.discovery["source"]["uid"] = ""
                    else:
                        api.discovery["contract"] = ""
                report = readiness.assess(api, config, catalog)
                self.assertFalse(report["ready_for_creation_review"])
                self.assertNotIn("expected_preview_revision", report["components"][0])


if __name__ == "__main__":
    unittest.main()
