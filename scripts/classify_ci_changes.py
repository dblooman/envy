#!/usr/bin/env python3
"""Classify pull request changes for focused CI profiles."""

import os
import re
import subprocess
import sys
from pathlib import Path


RELEASE_ONLY_PATHS = frozenset(
    {
        "deploy/helm/envy-quickstart/Chart.lock",
        "deploy/helm/envy-quickstart/Chart.yaml",
        "deploy/helm/envy-quickstart/values.yaml",
        "deploy/helm/envy/Chart.yaml",
        "deploy/helm/envy/values.yaml",
        "deploy/testing/quickstart-acceptance.py",
        "site/src/content/docs/getting-started/local-quickstart.md",
    }
)
MESH_REQUIRED = re.compile(
    r"^(?:internal/|cmd/|deploy/|examples/|scripts/|tests/|"
    r"go\.[^/]*$|Dockerfile\.[^/]*$|\.dockerignore$|Makefile$|"
    r"\.nvmrc$|web/(?:package\.json|pnpm-lock\.yaml)$|"
    r"\.github/workflows/(?:ci|mesh-acceptance)\.yml$)"
)
SITE_REQUIRED = re.compile(r"^(?:site/|\.nvmrc$)")


def classify(paths: list[str]) -> dict[str, bool]:
    release_only = bool(paths) and all(path in RELEASE_ONLY_PATHS for path in paths)
    return {
        "release_only": release_only,
        "site_required": any(SITE_REQUIRED.search(path) for path in paths),
        "mesh_required": not release_only
        and any(MESH_REQUIRED.search(path) for path in paths),
    }


def main() -> None:
    if len(sys.argv) != 3:
        raise SystemExit("usage: classify_ci_changes.py BASE_SHA HEAD_SHA")

    base_sha, head_sha = sys.argv[1:]
    changed_paths = subprocess.run(
        ["git", "diff", "--name-only", f"{base_sha}...{head_sha}"],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.splitlines()
    result = classify(changed_paths)
    output = "".join(f"{key}={str(value).lower()}\n" for key, value in result.items())
    output_path = Path(os.environ["GITHUB_OUTPUT"])
    with output_path.open("a", encoding="utf-8") as stream:
        stream.write(output)


if __name__ == "__main__":
    main()
