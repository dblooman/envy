"""Regression tests for focused release pull request checks."""

import unittest

from classify_ci_changes import classify


class ClassifyCIChangesTest(unittest.TestCase):
    def test_release_metadata_and_docs_use_focused_profile(self):
        result = classify(
            [
                "deploy/helm/envy-quickstart/Chart.lock",
                "deploy/helm/envy-quickstart/Chart.yaml",
                "deploy/helm/envy-quickstart/values.yaml",
                "deploy/helm/envy/Chart.yaml",
                "deploy/helm/envy/values.yaml",
                "deploy/testing/quickstart-acceptance.py",
                "site/src/content/docs/getting-started/local-quickstart.md",
            ]
        )

        self.assertEqual(
            {
                "release_only": True,
                "site_required": True,
                "mesh_required": False,
            },
            result,
        )

    def test_mixed_changes_use_full_profile(self):
        result = classify(
            ["deploy/helm/envy/Chart.yaml", "internal/server/server.go"]
        )

        self.assertEqual(
            {
                "release_only": False,
                "site_required": False,
                "mesh_required": True,
            },
            result,
        )

    def test_unknown_path_uses_full_profile(self):
        result = classify(["deploy/helm/envy/Chart.yaml", "CHANGELOG.md"])

        self.assertFalse(result["release_only"])

    def test_empty_change_set_uses_full_profile(self):
        result = classify([])

        self.assertFalse(result["release_only"])
        self.assertFalse(result["mesh_required"])


if __name__ == "__main__":
    unittest.main()
