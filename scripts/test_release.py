"""Static release-workflow regression checks."""

from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = (ROOT / ".github/workflows/release.yml").read_text(encoding="utf-8")
CHART = (ROOT / "deploy/helm/envy/Chart.yaml").read_text(encoding="utf-8")
VALUES = (ROOT / "deploy/helm/envy/values.yaml").read_text(encoding="utf-8")


class ReleaseWorkflowTest(unittest.TestCase):
    def test_quickstart_release_contract(self):
        quickstart = (ROOT / "deploy/helm/envy-quickstart/Chart.yaml").read_text()
        values = (ROOT / "deploy/helm/envy-quickstart/values.yaml").read_text()
        version = re.search(r"^version: (\S+)$", CHART, re.MULTILINE).group(1)
        self.assertIn(f"version: {version}", quickstart)
        self.assertIn(f'appVersion: "{version}"', quickstart)
        self.assertIn(f'baselineTag: "{version}-v1"', values)
        self.assertIn(f'previewTag: "{version}-v2"', values)
        for required in ("scripts/package-quickstart.sh dist", "dist/envy-quickstart-*.tgz", "davey/envy-demo:${{ steps.version.outputs.version }}-v1", "davey/envy-demo:${{ steps.version.outputs.version }}-v2"):
            self.assertIn(required, WORKFLOW)

    def test_release_contract(self):
        for text in (
            "tags: [\"v*\"]",
            "^v([0-9]+)\\.([0-9]+)\\.([0-9]+)$",
            "must be an annotated tag",
            "git ls-remote --tags origin",
            "davey/envy",
            "CHART_NAMESPACE: davey",
            "linux/amd64,linux/arm64",
            "provenance: mode=max",
            "SHA256SUMS",
            "GOOS=\"$os\" GOARCH=\"$arch\"",
            "build windows amd64 .exe zip",
            "helm push",
        ):
            self.assertIn(text, WORKFLOW)

    def test_pinned_actions_and_docker_hub_secret(self):
        for action in (
            "actions/checkout@",
            "docker/setup-buildx-action@",
            "docker/login-action@",
            "docker/build-push-action@",
            "softprops/action-gh-release@",
        ):
            self.assertIn(action, WORKFLOW)
        self.assertIn("secrets.DOCKER_HUB", WORKFLOW)

    def test_chart_defaults_match_the_release_contract(self):
        chart_version = re.search(r"^version: ([^\s]+)$", CHART, re.MULTILINE)
        app_version = re.search(r'^appVersion: "([^"]+)"$', CHART, re.MULTILINE)
        image_repo = re.search(r"^  repository: ([^\s]+)$", VALUES, re.MULTILINE)
        image_tag = re.search(r'^  tag: "([^"]+)"$', VALUES, re.MULTILINE)
        self.assertIsNotNone(chart_version)
        self.assertIsNotNone(app_version)
        self.assertIsNotNone(image_repo)
        self.assertIsNotNone(image_tag)
        self.assertEqual(chart_version.group(1), app_version.group(1))
        self.assertEqual(chart_version.group(1), image_tag.group(1))
        self.assertEqual("davey/envy", image_repo.group(1))
        self.assertEqual("envy-chart", re.search(r"^name: ([^\s]+)$", CHART, re.MULTILINE).group(1))


if __name__ == "__main__":
    unittest.main()
