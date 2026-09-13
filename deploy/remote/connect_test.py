import copy
import unittest
from connect import forwarded_config, remote_command


class ConnectionTests(unittest.TestCase):
    def setUp(self):
        self.config = {"clusters": [{"name": "kind-envy-dev", "cluster": {
            "server": "https://127.0.0.1:54321", "certificate-authority-data": "test-ca"}}],
            "users": [{"name": "kind", "user": {"client-certificate-data": "cert", "client-key-data": "key"}}]}

    def test_only_dial_address_changes(self):
        expected = copy.deepcopy(self.config)
        expected["clusters"][0]["cluster"]["server"] = "https://127.0.0.1:18443"
        actual, remote_port = forwarded_config(self.config, 18443)
        self.assertEqual(actual, expected)
        self.assertEqual(remote_port, 54321)

    def test_rejects_nonlocal_and_insecure_endpoints(self):
        for endpoint in ["http://127.0.0.1:1234", "https://example.com:1234", "https://127.0.0.1"]:
            config = copy.deepcopy(self.config)
            config["clusters"][0]["cluster"]["server"] = endpoint
            with self.assertRaises(ValueError):
                forwarded_config(config, 18443)
        self.config["clusters"][0]["cluster"]["insecure-skip-tls-verify"] = True
        with self.assertRaises(ValueError):
            forwarded_config(self.config, 18443)

    def test_does_not_import_executable_credentials(self):
        self.config["users"][0]["user"]["exec"] = {"command": "unexpected"}
        with self.assertRaises(ValueError):
            forwarded_config(self.config, 18443)

    def test_quotes_remote_paths(self):
        command = remote_command("/Users/test/Envy $(touch nope)", ["cat", "a'b"])
        self.assertIn("'/Users/test/Envy $(touch nope)'", command)
        self.assertTrue(command.endswith("cat 'a'\"'\"'b'"))


if __name__ == "__main__":
    unittest.main()
