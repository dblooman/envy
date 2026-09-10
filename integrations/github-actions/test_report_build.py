import importlib.util
import json
from pathlib import Path
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

spec = importlib.util.spec_from_file_location("report_build", Path(__file__).with_name("report-build.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ReportTests(unittest.TestCase):
    def test_scope_body_and_no_redirect(self):
        received = []

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def do_POST(self):
                received.append((self.path, self.headers.get("Authorization"), self.rfile.read(int(self.headers["Content-Length"]))))
                if len(received) == 2:
                    self.send_response(302)
                    self.send_header("Location", "/leak-token")
                    self.end_headers()
                    return
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b'{"id":"recorded"}')

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            env = {"ENVY_API_URL": f"http://127.0.0.1:{server.server_port}", "ENVY_BUILD_TOKEN": "scoped-secret", "ENVY_PROJECT": "shop", "ENVY_REPOSITORY": "backend"}
            with tempfile.TemporaryDirectory() as directory:
                file = Path(directory) / "report.json"
                body = json.dumps({"component": "pricing", "revision": "a" * 40}).encode()
                file.write_bytes(body)
                self.assertEqual(module.report(file, env), {"id": "recorded"})
                self.assertEqual(received[0], ("/v1/projects/shop/repositories/backend/builds", "Bearer scoped-secret", body))
                with self.assertRaisesRegex(ValueError, "HTTP 302"):
                    module.report(file, env)
                self.assertEqual(len(received), 2)
                self.assertEqual(received[1][2], body)
        finally:
            server.shutdown()
            thread.join()
            server.server_close()

    def test_missing_token_and_embedded_credentials_rejected(self):
        with self.assertRaisesRegex(ValueError, "ENVY_BUILD_TOKEN"):
            module.report("unused.json", {"ENVY_API_URL": "https://envy.example"})
        with self.assertRaisesRegex(ValueError, "without credentials"):
            module.report("unused.json", {"ENVY_API_URL": "https://secret@envy.example"})


if __name__ == "__main__":
    unittest.main()
