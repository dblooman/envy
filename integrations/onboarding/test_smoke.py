from contextlib import contextmanager, redirect_stderr, redirect_stdout
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest.mock import Mock, patch


SCRIPT = Path(__file__).with_name("smoke.py")
spec = importlib.util.spec_from_file_location("onboarding_smoke", SCRIPT)
smoke = importlib.util.module_from_spec(spec)
sys.path.insert(0, str(SCRIPT.parent))
try:
    spec.loader.exec_module(smoke)
finally:
    sys.path.pop(0)


def configuration(url="https://example.invalid/private-business-path"):
    targets = {}
    for index, name in enumerate(smoke.TARGETS, 1):
        targets[name] = {
            "url": url,
            "expect": {"/version": f"v{index}", "/shared": {"enabled": True}},
        }
        if name != "baseline":
            targets[name]["composition_id"] = name.replace("_", "-")
    return {
        "api_version": "envy-http-smoke/v1",
        "scenario": "synthetic-business-response",
        "rounds": 2,
        "timeout_seconds": 2,
        "targets": targets,
        "comparisons": [
            {"pointer": "/version", "relation": "distinct"},
            {"pointer": "/shared", "relation": "equal"},
        ],
    }


def document(target):
    index = {"": 1, "preview-a": 2, "preview-b": 3}[target.get("composition_id", "")]
    return {"version": f"v{index}", "shared": {"enabled": True}}


def empty_tokens():
    return dict.fromkeys(smoke.TARGETS, "")


@contextmanager
def http_server(respond):
    requests = []

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_GET(self):
            requests.append(
                {
                    "method": self.command,
                    "path": self.path,
                    "authorization": self.headers.get("Authorization"),
                    "baggage": self.headers.get("baggage"),
                    "accept": self.headers.get("Accept"),
                    "cache_control": self.headers.get("Cache-Control"),
                }
            )
            status, headers, body = respond(requests[-1])
            self.send_response(status)
            for key, value in headers.items():
                self.send_header(key, value)
            if "Content-Length" not in headers:
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
        yield f"http://127.0.0.1:{server.server_port}", requests
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


def call_main(*args):
    stdout, stderr = io.StringIO(), io.StringIO()
    with (
        patch.object(sys, "argv", [str(SCRIPT), *map(str, args)]),
        redirect_stdout(stdout),
        redirect_stderr(stderr),
    ):
        result = smoke.main()
    return result, stdout.getvalue(), stderr.getvalue()


class SmokeExecutionTests(unittest.TestCase):
    def test_cli_interleaves_business_assertions_and_scopes_credentials(self):
        secret_body = "private-body-field-value"
        secret_header = "private-header-value"

        def respond(request):
            composition = (request["baggage"] or "").removeprefix("composition=")
            body = document({"composition_id": composition})
            body["unasserted-private-field"] = secret_body
            return (
                200,
                {"Content-Type": "application/json", "X-Private": secret_header},
                json.dumps(body).encode(),
            )

        with (
            http_server(respond) as (origin, requests),
            tempfile.TemporaryDirectory() as directory,
        ):
            directory = Path(directory)
            config = configuration(origin + "/private-business-path")
            for name, target in config["targets"].items():
                target["token_file"] = name + ".token"
                (directory / target["token_file"]).write_text("private-token-" + name)
            config_path, output = directory / "input.json", directory / "report.json"
            config_path.write_text(json.dumps(config))
            result = subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "--config",
                    str(config_path),
                    "--output",
                    str(output),
                ],
                text=True,
                capture_output=True,
                timeout=10,
                check=False,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            report_text = output.read_text()
            report = json.loads(report_text)
            self.assertTrue(report["passed"])
            self.assertEqual(len(report["rounds"]), 2)
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)
            self.assertEqual(len(requests), 10)
            for request, name in zip(requests, smoke.ORDER * 2):
                self.assertEqual(
                    request["authorization"], "Bearer private-token-" + name
                )
                self.assertEqual(
                    request["baggage"],
                    None
                    if name == "baseline"
                    else "composition=" + name.replace("_", "-"),
                )
                self.assertEqual(request["method"], "GET")
                self.assertEqual(request["accept"], "application/json")
                self.assertEqual(request["cache_control"], "no-cache")
            for secret in (
                secret_body,
                secret_header,
                "private-token-",
                "private-business-path",
                origin,
                "composition=",
                '"version"',
                '"enabled"',
            ):
                self.assertNotIn(secret, report_text + result.stdout + result.stderr)

    def test_failed_final_request_discards_stale_document_and_rounds_do_not_mask_failure(
        self,
    ):
        calls = 0

        def request(target, _token, _timeout):
            nonlocal calls
            calls += 1
            if calls == 5:
                raise smoke.CheckError("transport_error")
            return document(target)

        report = smoke.run(configuration(), empty_tokens(), request)
        self.assertFalse(report["passed"])
        self.assertFalse(report["rounds"][0]["passed"])
        self.assertTrue(report["rounds"][1]["passed"])
        self.assertEqual(
            report["rounds"][0]["requests"][-1]["error"], "transport_error"
        )
        self.assertTrue(
            all(
                c["error"] == "comparison_unavailable"
                for c in report["rounds"][0]["comparisons"]
            )
        )

    def test_every_interleaved_baseline_assertion_must_pass(self):
        calls = 0

        def request(target, _token, _timeout):
            nonlocal calls
            calls += 1
            result = document(target)
            if calls == 3:
                result["version"] = "unexpected-business-response"
            return result

        report = smoke.run(configuration(), empty_tokens(), request)
        first = report["rounds"][0]
        self.assertFalse(report["passed"])
        self.assertTrue(all(c["passed"] for c in first["comparisons"]))
        self.assertEqual(first["requests"][2]["failed_assertions"], [1])
        self.assertEqual(first["requests"][2]["error"], "expectation_mismatch")
        self.assertNotIn("unexpected-business-response", json.dumps(report))

    def test_missing_pointer_is_not_equal_to_present_null(self):
        config = configuration()
        for target in config["targets"].values():
            target["expect"] = {"/nullable": None, "/absent": None}
        config["comparisons"] = [{"pointer": "/absent", "relation": "equal"}]
        report = smoke.run(config, empty_tokens(), lambda *_: {"nullable": None})
        self.assertFalse(report["passed"])
        self.assertEqual(report["rounds"][0]["requests"][0]["failed_assertions"], [2])
        self.assertEqual(
            report["rounds"][0]["comparisons"][0]["error"], "comparison_unavailable"
        )

    def test_nested_booleans_and_numbers_are_distinct_json_values(self):
        config = configuration()
        config["targets"]["baseline"]["expect"]["/shared"] = {"enabled": 1}
        report = smoke.run(config, empty_tokens(), lambda target, *_: document(target))
        self.assertFalse(report["passed"])
        self.assertEqual(report["rounds"][0]["requests"][0]["failed_assertions"], [2])
        comparisons = smoke.compare(
            {"baseline": {"nested": [True]}, "preview_a": {"nested": [1]}},
            [
                {
                    "pointer": "/nested",
                    "relation": "distinct",
                    "targets": ["baseline", "preview_a"],
                }
            ],
        )
        self.assertTrue(comparisons[0]["passed"])

    def test_comparisons_respect_selected_targets_and_pairwise_distinctness(self):
        docs = {
            "baseline": {"value": "v1"},
            "preview_a": {"value": "v2"},
            "preview_b": {"value": "v2"},
        }
        rules = [
            {"pointer": "/value", "relation": "distinct"},
            {
                "pointer": "/value",
                "relation": "distinct",
                "targets": ["baseline", "preview_a"],
            },
            {
                "pointer": "/value",
                "relation": "equal",
                "targets": ["preview_a", "preview_b"],
            },
        ]
        self.assertEqual(
            [r["passed"] for r in smoke.compare(docs, rules)], [False, True, True]
        )

    def test_json_pointer_escapes_root_and_array_indices(self):
        doc = {"a/b": {"~key": [None, {"": "value"}]}}
        self.assertIs(smoke.select(doc, ""), doc)
        self.assertIsNone(smoke.select(doc, "/a~1b/~0key/0"))
        self.assertEqual(smoke.select(doc, "/a~1b/~0key/1/"), "value")
        for pointer in ("/a~1b/~0key/01", "/a~1b/~0key/-1", "/a~1b/~0key/2", "/absent"):
            with (
                self.subTest(pointer=pointer),
                self.assertRaisesRegex(smoke.CheckError, "^missing_pointer$"),
            ):
                smoke.select(doc, pointer)


class SmokeHTTPTests(unittest.TestCase):
    def test_redirects_never_forward_credentials_or_baggage(self):
        with http_server(
            lambda _: (200, {"Content-Type": "application/json"}, b"{}")
        ) as (destination, forwarded):
            for status in (301, 302, 303, 307, 308):
                with (
                    self.subTest(status=status),
                    http_server(
                        lambda _: (
                            status,
                            {"Location": destination + "/private"},
                            b"private-redirect-body",
                        )
                    ) as (origin, requests),
                ):
                    with self.assertRaisesRegex(
                        smoke.CheckError, "^(redirect_refused|unexpected_status)$"
                    ):
                        smoke.fetch(
                            {"url": origin, "composition_id": "preview-a"},
                            "private-token",
                            2,
                        )
                    self.assertEqual(len(requests), 1)
            self.assertEqual(forwarded, [])

    def test_http_failures_are_fixed_codes_without_response_details(self):
        cases = [
            (
                403,
                "application/json",
                b'{"private":"error-detail"}',
                "unexpected_status",
            ),
            (204, "application/json", b"", "unexpected_status"),
            (200, "text/html", b"private-html", "non_json_content_type"),
            (200, "application/json", b"private-invalid-json", "invalid_json"),
            (200, "application/json", b'{"duplicate":1,"duplicate":2}', "invalid_json"),
            (200, "application/json", b'{"nested":[NaN]}', "invalid_json"),
            (200, "application/json", b'{"nested":[1e999]}', "invalid_json"),
            (
                200,
                "application/json",
                b" " * (smoke.MAX_BYTES + 1),
                "response_too_large",
            ),
        ]
        for status, content_type, body, code in cases:
            with (
                self.subTest(code=code, body_size=len(body)),
                http_server(
                    lambda _: (status, {"Content-Type": content_type}, body)
                ) as (origin, _),
            ):
                with self.assertRaisesRegex(smoke.CheckError, "^" + code + "$"):
                    smoke.fetch(
                        {"url": origin + "/private-endpoint"}, "private-token", 2
                    )

    def test_vendor_json_and_exact_response_limit_are_accepted(self):
        body = b"{}" + b" " * (smoke.MAX_BYTES - 2)
        with http_server(
            lambda _: (
                200,
                {"Content-Type": "application/problem+json; charset=utf-8"},
                body,
            )
        ) as (origin, requests):
            self.assertEqual(smoke.fetch({"url": origin}, "", 2), {})
            self.assertIsNone(requests[0]["authorization"])
            self.assertIsNone(requests[0]["baggage"])

    def test_transport_failures_are_sanitized(self):
        server = ThreadingHTTPServer(("127.0.0.1", 0), BaseHTTPRequestHandler)
        origin = f"http://127.0.0.1:{server.server_port}/private-endpoint"
        server.server_close()
        with self.assertRaisesRegex(smoke.CheckError, "^transport_error$"):
            smoke.fetch({"url": origin}, "private-token", 1)

    def test_premature_eof_cannot_pass_even_with_a_valid_json_prefix(self):
        with http_server(
            lambda _: (
                200,
                {"Content-Type": "application/json", "Content-Length": "1000"},
                b'{"ok":true}',
            )
        ) as (origin, _):
            with self.assertRaisesRegex(smoke.CheckError, "^incomplete_response$"):
                smoke.fetch({"url": origin}, "", 2)


class SmokeInputTests(unittest.TestCase):
    def load(self, config):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "input.json"
            path.write_text(json.dumps(config))
            return smoke.load_config(path)

    def test_valid_defaults_and_equal_only_comparisons(self):
        config = configuration()
        config.pop("rounds")
        config.pop("timeout_seconds")
        config["comparisons"] = [{"pointer": "/shared", "relation": "equal"}]
        validated = self.load(config)
        self.assertEqual(validated["rounds"], 2)
        self.assertEqual(validated["timeout_seconds"], 10)
        self.assertTrue(
            smoke.run(validated, empty_tokens(), lambda target, *_: document(target))[
                "passed"
            ]
        )

    def test_schema_and_execution_bounds_fail_closed(self):
        changes = [
            lambda c: c.update(unrecognized=True),
            lambda c: c.update(api_version="unknown"),
            lambda c: c.update(scenario="private scenario"),
            lambda c: c.update(rounds=True),
            lambda c: c.update(rounds=0),
            lambda c: c.update(rounds=11),
            lambda c: c.update(timeout_seconds=31),
            lambda c: c.update(comparisons=[]),
            lambda c: c.update(comparisons=c["comparisons"] * 17),
            lambda c: c["targets"].pop("preview_b"),
            lambda c: c["targets"]["baseline"].update(composition_id="preview-a"),
            lambda c: c["targets"]["preview_a"].pop("composition_id"),
            lambda c: c["targets"]["preview_a"].update(composition_id="invalid,value"),
            lambda c: c["targets"]["baseline"].update(expect={}),
            lambda c: c["targets"]["baseline"].update(
                expect={f"/key{i}": i for i in range(33)}
            ),
            lambda c: c["targets"]["baseline"].update(expect={"/bad~2": 1}),
            lambda c: c["targets"]["baseline"].update(expect={"/" + "x" * 512: 1}),
            lambda c: c["targets"]["baseline"].update(headers={"private": "value"}),
            lambda c: c["comparisons"][0].update(targets=["baseline"]),
            lambda c: c["comparisons"][0].update(targets=["baseline", "baseline"]),
            lambda c: c["comparisons"][0].update(targets=["baseline", "unknown"]),
            lambda c: c["comparisons"][0].update(relation="contains"),
        ]
        for index, change in enumerate(changes):
            with self.subTest(case=index):
                config = configuration()
                change(config)
                with self.assertRaises(ValueError):
                    self.load(config)

    def test_unsafe_target_urls_are_rejected(self):
        for url in (
            "http://example.invalid",
            "https://user:secret@example.invalid",
            "https://example.invalid/path?secret=value",
            "https://example.invalid/#private",
            "https://example.invalid:bad/path",
            "https://example.invalid/white space",
            "file:///private/path",
            "https://",
        ):
            with self.subTest(url=url):
                config = configuration()
                config["targets"]["baseline"]["url"] = url
                with self.assertRaises(ValueError):
                    self.load(config)

    def test_oversized_and_ambiguous_json_configuration_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "input.json"
            for raw in (
                b" " * (smoke.MAX_BYTES + 1),
                b'{"api_version":1,"api_version":2}',
                b'{"nested":[NaN]}',
                b'{"nested":[Infinity]}',
                b'{"nested":[1e999]}',
            ):
                with self.subTest(raw_size=len(raw)):
                    path.write_bytes(raw)
                    with self.assertRaises(ValueError):
                        smoke.load_config(path)

    def test_invalid_tokens_are_rejected_without_disclosure_or_requests(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            config = configuration()
            config["targets"]["baseline"]["token_file"] = "private.token"
            path = directory / "input.json"
            path.write_text(json.dumps(config))
            for token in (
                "",
                "private token",
                "private\ntoken",
                "private\x00token",
                "private-☃",
            ):
                with (
                    self.subTest(token_type=repr(token)),
                    patch.object(smoke, "run") as run,
                ):
                    (directory / "private.token").write_text(token)
                    result, out, err = call_main("--config", path)
                    self.assertEqual(result, 1)
                    self.assertEqual(out, "")
                    self.assertNotIn("private", err)
                    run.assert_not_called()

    def test_check_config_does_not_read_tokens_or_send_requests(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "input.json"
            config = configuration()
            config["targets"]["preview_a"]["token_file"] = "missing.token"
            path.write_text(json.dumps(config))
            with (
                patch.object(smoke, "tokens_for") as tokens,
                patch.object(smoke, "run") as run,
            ):
                result, out, err = call_main("--config", path, "--check-config")
            self.assertEqual((result, err), (0, ""))
            self.assertIn("no HTTP requests", out)
            tokens.assert_not_called()
            run.assert_not_called()

    def test_output_cannot_overwrite_configuration_or_tokens_even_through_symlinks(
        self,
    ):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            config = configuration()
            config["targets"]["baseline"]["token_file"] = "credential"
            config_path, token_path = directory / "input.json", directory / "credential"
            config_path.write_text(json.dumps(config))
            token_path.write_text("private-token")
            alias = directory / "alias.json"
            alias.symlink_to(token_path)
            for output in (config_path, token_path, alias):
                before = output.read_bytes()
                with (
                    self.subTest(output=output.name),
                    patch.object(smoke, "run") as run,
                ):
                    result, out, err = call_main(
                        "--config", config_path, "--output", output
                    )
                    self.assertEqual(result, 1)
                    self.assertEqual(out, "")
                    self.assertNotIn("private-token", err)
                    self.assertEqual(output.read_bytes(), before)
                    run.assert_not_called()

    def test_cli_failure_exit_and_invalid_input_are_sanitized(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "input.json"
            config = configuration()
            path.write_text(json.dumps(config))
            failed = smoke.run(
                config,
                empty_tokens(),
                Mock(side_effect=smoke.CheckError("transport_error")),
            )
            with patch.object(smoke, "run", return_value=failed):
                result, out, err = call_main("--config", path)
            self.assertEqual((result, err), (2, ""))
            self.assertFalse(json.loads(out)["passed"])
            self.assertNotIn("private-business-path", out)
            path.write_text('{"private-input":"do-not-disclose", invalid}')
            result, out, err = call_main("--config", path)
            self.assertEqual(result, 1)
            self.assertEqual(out, "")
            self.assertNotIn("do-not-disclose", err)
            self.assertNotIn("private-input", err)


if __name__ == "__main__":
    unittest.main()
