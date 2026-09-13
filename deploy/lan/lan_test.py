import copy
import json
from pathlib import Path
import subprocess
import unittest

from acceptance import check_chain, Client
from render import load_builds, render


class LANTests(unittest.TestCase):
    def test_catalog_and_workloads_share_immutable_bindings(self):
        builds = load_builds(Path(__file__).with_name("builds.json"))
        workloads, catalog = render(builds)
        deployments = {o["metadata"]["name"]: o for o in workloads["items"] if o["kind"] == "Deployment"}
        for name, binding in catalog["baseline"]["components"].items():
            pod = deployments[name]["spec"]["template"]["spec"]
            self.assertEqual(pod["containers"][0]["image"], binding["image"])
            self.assertEqual(pod["imagePullSecrets"], [{"name": "envy-ghcr"}])
        self.assertEqual(catalog["baseline"]["verification"]["kind"], "http")
        self.assertEqual([c["id"] for c in catalog["components"] if c["overridable"]], ["pricing"])

    def test_context_and_business_changes_are_required(self):
        headers = {"x-shop-storefront-context": "cmp", "x-shop-pricing-context": "cmp", "x-shop-storefront-workload": "main-a", "x-shop-pricing-workload": "branch-b"}
        observation = (200, headers, {"price_minor": 1050})
        self.assertEqual(check_chain(observation, "cmp", 1050), ["main-a", "branch-b"])
        with self.assertRaises(RuntimeError):
            check_chain(observation, "cmp", 1090)
        dropped = copy.deepcopy(headers); dropped["x-shop-pricing-context"] = ""
        with self.assertRaises(RuntimeError):
            check_chain((200, dropped, observation[2]), "cmp", 1050)

    def test_endpoint_rejects_embedded_credentials(self):
        with self.assertRaises(ValueError):
            Client("http://user:password@host", "http://host", "token")

    def test_chart_http_and_tls_preflight(self):
        helm = Path(".envy/bin/helm")
        if not helm.exists():
            self.skipTest("local Helm unavailable")
        base = [str(helm), "template", "envy", "deploy/helm/envy", "-f", "deploy/lan/values.yaml", "--set", "preflight.enabled=true"]
        http = subprocess.check_output(base, text=True)
        self.assertIn('name: PUBLIC_PORT, value: "30080"', http)
        self.assertIn('name: INGRESS_PORT, value: "80"', http)
        self.assertIn("nodePort: 30081", http)
        self.assertIn('SELECT 1', http)
        self.assertNotIn('pg_isready', http)
        tls = subprocess.check_output(base + ["--set", "runtime.previewBaseURL=https://envy.test:8443", "--set", "runtime.ingressURL=https://ingress:9443", "--set", "runtime.caConfigMap.name=private-ca"], text=True)
        self.assertIn('name: PUBLIC_PORT, value: "8443"', tls)
        self.assertIn('name: INGRESS_PORT, value: "9443"', tls)
        self.assertIn('CURL_CA_BUNDLE', tls)
        self.assertNotIn('--insecure', tls)

class RESTClientTests(unittest.TestCase):
    def test_host_routing_and_cleanup_over_http(self):
        self.exercise(False)

    def test_wrong_branch_response_fails_and_requests_cleanup(self):
        self.exercise(True)

    def exercise(self, wrong_branch):
        from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
        import threading
        from acceptance import accept
        builds = load_builds(Path(__file__).with_name("builds.json"))
        state = {"created": 0, "destroyed": False, "hosts": []}
        composition_id = "abc123"
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass
            def reply(self, code, value, headers=None):
                self.send_response(code)
                for k, v in (headers or {}).items():
                    self.send_header(k, v)
                self.end_headers(); self.wfile.write(json.dumps(value).encode())
            def composition(self):
                return {"id": composition_id, "phase": "destroyed" if state["destroyed"] else "ready", "endpoints": {"public": {"ready": True, "url": "http://cmp-abc123.envy.test:30080"}}, "verification_level": "http"}
            def do_POST(self):
                if self.headers.get("Authorization") != "Bearer test-token":
                    self.reply(401, {}); return
                body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                if body["overrides"]["pricing"]["image"] != builds["pricing_branch"]["image"]:
                    self.reply(400, {}); return
                state["created"] += 1
                self.reply(202, self.composition())
            def do_DELETE(self):
                state["destroyed"] = True
                self.reply(202, self.composition())
            def do_GET(self):
                if self.path.startswith("/v1/"):
                    if self.headers.get("Authorization") != "Bearer test-token":
                        self.reply(401, {}); return
                    self.reply(200, self.composition() if "/compositions/" in self.path else {"principal": {"id": "lan-client", "kind": "service"}}); return
                host = self.headers.get("Host")
                state["hosts"].append(host)
                preview = host == "cmp-abc123.envy.test:30080"
                if host not in ("baseline.envy.test:30080", "cmp-abc123.envy.test:30080") or (preview and state["destroyed"]):
                    self.reply(404, {}); return
                context = composition_id if preview else ""
                headers = {"X-Shop-Storefront-Context": context, "X-Shop-Pricing-Context": context, "X-Shop-Storefront-Workload": "main-a", "X-Shop-Pricing-Workload": "branch-b" if preview else "main-b"}
                price = 1050 if preview and not wrong_branch else 1090
                self.reply(200, {"price_minor": price}, headers)
        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        worker = threading.Thread(target=server.serve_forever, daemon=True); worker.start()
        try:
            origin = f"http://127.0.0.1:{server.server_port}"
            report = {}
            if wrong_branch:
                with self.assertRaises(RuntimeError):
                    accept(Client(origin, origin, "test-token"), builds, report, timeout=5)
                self.assertNotEqual(report.get("result"), "passed")
            else:
                accept(Client(origin, origin, "test-token"), builds, report, timeout=5)
                self.assertEqual(report["result"], "passed")
                self.assertIn("unknown.envy.test:30080", state["hosts"])
            self.assertTrue(state["destroyed"])
            self.assertEqual(state["created"], 2)  # One intent, retried with the same key.
        finally:
            server.shutdown(); server.server_close(); worker.join()
