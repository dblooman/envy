import importlib.util
import json
import os
from pathlib import Path
from types import SimpleNamespace
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import parse_qs, urlsplit


spec = importlib.util.spec_from_file_location("github_actions_preview", Path(__file__).with_name("preview.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class FakeEnvy:
    def __init__(self):
        self.baseline_revision = "baseline-rev-1"
        self.create_calls = []
        self.update_calls = []
        self.delete_calls = 0
        self.catalog_calls = 0
        self.status_calls = 0
        self.status_mode = "ready"
        self.endpoint_ready = True
        self.auth_seen = []
        self.server = HTTPServer(("127.0.0.1", 0), self.handler())
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def close(self):
        self.server.shutdown()
        self.thread.join()
        self.server.server_close()

    def handler(self):
        fake = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def send_json(self, status, value):
                body = json.dumps(value).encode()
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def read_json(self):
                return json.loads(self.rfile.read(int(self.headers["Content-Length"])))

            def do_GET(self):
                fake.auth_seen.append(self.headers.get("Authorization"))
                parsed = urlsplit(self.path)
                query = parse_qs(parsed.query)
                if parsed.path == "/v1/projects":
                    if query.get("after") == ["project-cursor"]:
                        self.send_json(200, {"items": [{"id": "demo", "name": "Demo"}]})
                    else:
                        self.send_json(200, {"items": [{"id": "other", "name": "Other"}], "next_cursor": "project-cursor"})
                    return
                if parsed.path == "/v1/projects/demo/baselines":
                    if query.get("after") == ["baseline-cursor"]:
                        self.send_json(
                            200,
                            {
                                "items": [
                                    {
                                        "id": "staging",
                                        "project": "demo",
                                        "revision": fake.baseline_revision,
                                    }
                                ]
                            },
                        )
                    else:
                        self.send_json(
                            200,
                            {
                                "items": [{"id": "other", "revision": "old"}],
                                "next_cursor": "baseline-cursor",
                            },
                        )
                    return
                if parsed.path == "/v1/compositions/cmp-1/status":
                    fake.status_calls += 1
                    if fake.status_mode == "timeout":
                        phase = "provisioning"
                    elif fake.status_mode == "failed":
                        phase = "failed"
                    elif fake.status_mode == "destroy":
                        phase = "destroyed" if fake.status_calls > 1 else "destroying"
                    else:
                        phase = "ready" if fake.status_calls > 1 else "provisioning"
                    self.send_json(
                        200,
                        {
                            "id": "cmp-1",
                            "phase": phase,
                            "generation": 1,
                            "observed_generation": 1 if phase == "ready" else 0,
                            "conditions": [],
                            "latest_operation": {"id": "op-1", "kind": "create", "status": phase},
                        },
                    )
                    return
                if parsed.path == "/v1/compositions/cmp-1/endpoints":
                    self.send_json(
                        200,
                        {
                            "id": "cmp-1",
                            "endpoints": {
                                "public": {
                                    "url": "https://cmp-1.preview.example",
                                    "ready": fake.endpoint_ready,
                                }
                            },
                        },
                    )
                    return
                if parsed.path == "/v1/compositions/cmp-1":
                    self.send_json(
                        200,
                        {
                            "id": "cmp-1",
                            "project": "demo",
                            "baseline": "staging",
                            "baseline_revision": fake.baseline_revision,
                            "generation": 1,
                            "phase": "ready",
                            "overrides": {},
                        },
                    )
                    return
                if parsed.path == "/bad":
                    self.send_json(500, {"error": "Bearer sensitive-token and registry-password"})
                    return
                if parsed.path == "/malformed":
                    self.send_response(200)
                    self.end_headers()
                    self.wfile.write(b"not-json")
                    return
                self.send_json(404, {"error": "not found"})

            def do_POST(self):
                fake.auth_seen.append(self.headers.get("Authorization"))
                parsed = urlsplit(self.path)
                if parsed.path == "/v1/catalog/validate":
                    fake.catalog_calls += 1
                    self.send_json(200, {"applied": False, "checks": [], "warnings": []})
                    return
                if parsed.path == "/v1/compositions":
                    body = self.read_json()
                    fake.create_calls.append((self.headers.get("Idempotency-Key"), body))
                    self.send_json(
                        202,
                        {
                            "id": "cmp-1",
                            "project": body["project"],
                            "baseline": body["baseline"],
                            "baseline_revision": body["expected_baseline_revision"],
                            "generation": 1,
                            "phase": "created",
                            "overrides": body["overrides"],
                        },
                    )
                    return
                self.send_json(404, {"error": "not found"})

            def do_PATCH(self):
                fake.auth_seen.append(self.headers.get("Authorization"))
                if urlsplit(self.path).path != "/v1/compositions/cmp-1":
                    self.send_json(404, {"error": "not found"})
                    return
                body = self.read_json()
                fake.update_calls.append((self.headers.get("Idempotency-Key"), body))
                if body.get("expected_generation") != 1:
                    self.send_json(409, {"error": "stale generation; sensitive-token"})
                    return
                self.send_json(202, {"id": "cmp-1", "generation": 2, "phase": "updating"})

            def do_DELETE(self):
                fake.auth_seen.append(self.headers.get("Authorization"))
                if urlsplit(self.path).path == "/v1/compositions/cmp-1":
                    fake.delete_calls += 1
                    fake.status_mode = "destroy"
                    fake.status_calls = 0
                    self.send_json(202, {"id": "cmp-1", "phase": "destroying"})
                    return
                self.send_json(404, {"error": "not found"})

        return Handler


class PreviewHelperTests(unittest.TestCase):
    def setUp(self):
        self.fake = FakeEnvy()
        self.api = module.API(
            "http://127.0.0.1:%d" % self.fake.server.server_port,
            "sensitive-token",
        )

    def tearDown(self):
        self.fake.close()

    def test_discovery_create_retry_catalog_and_destroy(self):
        self.api = module.API("http://127.0.0.1:%d" % self.fake.server.server_port, "sensitive-token")
        discovery = self.api.discover("demo", "staging")
        self.assertEqual(discovery["baseline_revision"], "baseline-rev-1")
        manifest = {
            "api_version": "envy/v1",
            "project": {"id": "demo", "name": "Demo"},
            "components": [{}],
            "baseline": {},
        }
        self.assertEqual(self.api.validate_catalog(manifest)["applied"], False)
        request = {"service-b": {"build_id": "a" * 64}}
        first = self.api.create("demo", "staging", "pr-1", request, "baseline-rev-1", idempotency_key="retry-1")
        replay = self.api.create("demo", "staging", "pr-1", request, "baseline-rev-1", idempotency_key="retry-1")
        self.assertEqual(first["id"], replay["id"])
        self.assertEqual(self.fake.create_calls[0][0], "retry-1")
        self.assertEqual(self.fake.create_calls[0][1]["expected_baseline_revision"], "baseline-rev-1")
        self.assertEqual(self.api.wait("cmp-1", timeout=2, poll_seconds=0)["phase"], "ready")
        endpoints = self.api.endpoints("cmp-1")
        self.assertEqual(endpoints["endpoints"]["public"]["url"], "https://cmp-1.preview.example")
        self.assertEqual(self.api.destroy("cmp-1", timeout=5)["phase"], "destroyed")
        self.assertEqual(self.fake.delete_calls, 1)
        self.assertTrue(self.fake.auth_seen)

    def test_update_requires_current_baseline_and_sends_generation(self):
        self.api = module.API("http://127.0.0.1:%d" % self.fake.server.server_port, "sensitive-token")
        current = self.api.get("cmp-1")
        self.assertEqual(current["baseline_revision"], "baseline-rev-1")
        result = self.api.update("cmp-1", 1, {"service-b": {"image": "registry.example/service-b@sha256:" + "b" * 64}}, "update-1")
        self.assertEqual(result["generation"], 2)
        self.assertEqual(self.fake.update_calls[0][0], "update-1")
        self.assertEqual(self.fake.update_calls[0][1]["expected_generation"], 1)
        with self.assertRaises(module.APIError) as raised:
            self.api.update("cmp-1", 2, {"service-b": {"build_id": "b" * 64}}, "update-stale")
        self.assertEqual(raised.exception.status, 409)
        self.assertNotIn("sensitive-token", str(raised.exception))

    def test_baseline_revision_conflict_does_not_create(self):
        self.fake.baseline_revision = "baseline-rev-2"
        os.environ["PREVIEW_TEST_TOKEN"] = "sensitive-token"
        args = SimpleNamespace(
            api_url="http://127.0.0.1:%d" % self.fake.server.server_port,
            token_env="PREVIEW_TEST_TOKEN",
            request_timeout=5,
            project="demo",
            baseline="staging",
            mode="create",
            name="pr-conflict",
            composition_id="",
            expected_generation=None,
            expected_baseline_revision="baseline-rev-1",
            idempotency_key="conflict-1",
            overrides_json="{}",
            overrides_file=None,
            catalog_manifest_file=None,
            ttl="8h",
            timeout=5,
            state_file="",
        )
        try:
            with self.assertRaises(module.APIError) as raised:
                module.run_preview(args)
            self.assertEqual(raised.exception.status, 409)
            self.assertEqual(self.fake.create_calls, [])
        finally:
            os.environ.pop("PREVIEW_TEST_TOKEN", None)

    def test_timeout_failure_and_redacted_errors(self):
        self.api = module.API("http://127.0.0.1:%d" % self.fake.server.server_port, "sensitive-token")
        self.fake.status_mode = "timeout"
        with self.assertRaises(module.WaitTimeout) as raised:
            self.api.wait("cmp-1", timeout=1, poll_seconds=0)
        self.assertEqual(raised.exception.exit_code, 5)
        self.fake.status_mode = "failed"
        self.fake.status_calls = 0
        with self.assertRaises(module.LifecycleFailure) as raised:
            self.api.wait("cmp-1", timeout=2, poll_seconds=0)
        self.assertEqual(raised.exception.exit_code, 4)
        self.fake.endpoint_ready = False
        with self.assertRaisesRegex(module.APIError, "endpoint is not ready"):
            self.api.endpoints("cmp-1")
        for path in ("/bad", "/malformed"):
            with self.assertRaises(module.APIError) as raised:
                self.api.request("GET", path, description="fixture")
            self.assertNotIn("sensitive-token", str(raised.exception))
            self.assertNotIn("registry-password", str(raised.exception))

    def test_run_preview_writes_metadata_and_state(self):
        self.api = module.API("http://127.0.0.1:%d" % self.fake.server.server_port, "sensitive-token")
        state = Path(__file__).with_name(".preview-test-state.json")
        try:
            args = SimpleNamespace(
                api_url=self.api.base_url,
                token_env="PREVIEW_TEST_TOKEN",
                request_timeout=5,
                project="demo",
                baseline="staging",
                mode="create",
                name="pr-2",
                composition_id="",
                expected_generation=None,
                expected_baseline_revision="baseline-rev-1",
                idempotency_key="run-1",
                overrides_json=json.dumps({"service-b": {"build_id": "c" * 64}}),
                overrides_file=None,
                catalog_manifest_file=None,
                ttl="8h",
                timeout=5,
                state_file=str(state),
            )
            os.environ["PREVIEW_TEST_TOKEN"] = "sensitive-token"
            result = module.run_preview(args)
            self.assertEqual(result["composition_id"], "cmp-1")
            self.assertTrue(result["endpoint_ready"])
            self.assertEqual(json.loads(state.read_text())["preview_url"], "https://cmp-1.preview.example")
        finally:
            state.unlink(missing_ok=True)
            os.environ.pop("PREVIEW_TEST_TOKEN", None)


if __name__ == "__main__":
    unittest.main()
