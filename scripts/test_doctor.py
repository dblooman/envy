import socket
import unittest
from doctor import port_available, version


class DoctorTests(unittest.TestCase):
    def test_versions(self):
        self.assertEqual(version("go version go1.27.1 darwin/arm64"), (1, 27, 1))
        self.assertEqual(version("v24.8.0"), (24, 8, 0))
        with self.assertRaises(ValueError):
            version("not installed")

    def test_occupied_port(self):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            sock.listen()
            self.assertFalse(port_available(sock.getsockname()[1]))


class PrerequisiteTests(unittest.TestCase):
    def test_missing_docker_has_actionable_failure(self):
        from unittest.mock import patch
        import doctor
        import contextlib
        import io

        stderr = io.StringIO()
        with (
            patch("sys.argv", ["doctor", "--local"]),
            patch.dict("os.environ", {}, clear=True),
            patch.object(doctor.shutil, "which", return_value=None),
            patch.object(doctor, "port_available", return_value=True),
            contextlib.redirect_stderr(stderr),
        ):
            self.assertEqual(doctor.main(), 1)
        self.assertIn("Missing docker: install Docker", stderr.getvalue())

    def test_invalid_ports_fail_before_cluster_creation(self):
        from unittest.mock import patch
        import doctor
        import contextlib
        import io

        stderr = io.StringIO()
        with (
            patch("sys.argv", ["doctor", "--local"]),
            patch.dict("os.environ", {"ENVY_API_PORT": "invalid"}, clear=True),
            patch.object(doctor.shutil, "which", return_value=None),
            patch.object(doctor, "port_available", return_value=True),
            contextlib.redirect_stderr(stderr),
        ):
            self.assertEqual(doctor.main(), 1)
        self.assertIn("ENVY_API_PORT must be a port", stderr.getvalue())
