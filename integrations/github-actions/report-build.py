#!/usr/bin/env python3
"""Report an already-pushed image using an immutable JSON file and scoped CI token."""
import argparse
import json
import os
from pathlib import Path
import sys
import urllib.error
import urllib.parse
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def report(file, environment=os.environ):
    base = environment.get("ENVY_API_URL", "").rstrip("/")
    parsed = urllib.parse.urlsplit(base)
    if parsed.scheme not in ("http", "https") or not parsed.netloc or parsed.username or parsed.query or parsed.fragment:
        raise ValueError("ENVY_API_URL must be an HTTP(S) API URL without credentials, query, or fragment")
    token = environment.get("ENVY_BUILD_TOKEN", "").strip()
    if not token or any(c in token for c in "\r\n"):
        raise ValueError("ENVY_BUILD_TOKEN is required and must be one line")
    project = environment.get("ENVY_PROJECT", "")
    repository = environment.get("ENVY_REPOSITORY", "")
    if not project or not repository:
        raise ValueError("ENVY_PROJECT and ENVY_REPOSITORY are required")
    body = Path(file).read_bytes()
    if len(body) > 64 * 1024:
        raise ValueError("build report must be at most 64 KiB")
    if not isinstance(json.loads(body), dict):
        raise ValueError("build report must be a JSON object")
    path = "/v1/projects/" + urllib.parse.quote(project, safe="") + "/repositories/" + urllib.parse.quote(repository, safe="") + "/builds"
    req = urllib.request.Request(base + path, data=body, method="POST", headers={"Authorization": "Bearer " + token, "Content-Type": "application/json", "Accept": "application/json"})
    try:
        with urllib.request.build_opener(NoRedirect()).open(req, timeout=120) as response:
            return json.load(response)
    except urllib.error.HTTPError as exc:
        # Never echo upstream response bodies or credentials into CI logs.
        raise ValueError(f"Envy rejected the build report (HTTP {exc.code}); check repository scope, image availability, and report identity") from None
    except urllib.error.URLError:
        raise ValueError("Envy could not be reached; retry using the same report file") from None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--file", required=True, help="JSON build report; reuse unchanged when retrying")
    args = parser.parse_args()
    try:
        result = report(args.file)
    except (ValueError, OSError) as exc:
        print(str(exc), file=sys.stderr)
        return 1
    print(json.dumps(result))
    return 0


if __name__ == "__main__":
    sys.exit(main())
