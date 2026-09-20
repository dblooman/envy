#!/usr/bin/env python3
"""Operator-driven Actions artifact import and local frontend preview."""

import argparse
import functools
import http.server
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request


def command(args, cwd=None, env=None):
    result = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError(
            result.stderr.strip() or result.stdout.strip() or "Command failed"
        )
    return result.stdout


def save(path, value):
    temporary = path.with_suffix(".tmp")
    temporary.write_text(json.dumps(value, indent=2) + "\n")
    temporary.chmod(0o600)
    temporary.replace(path)


def update_overrides(composition, component, build_id):
    """Updates carry the full override set; provenance fields are server-owned."""
    result = {}
    for name, override in composition["overrides"].items():
        field = "build_id" if override.get("build_id") else "image"
        result[name] = {field: override[field]}
    result[component] = {"build_id": build_id}
    return result


def validate_report(report, run, run_id, component):
    if run["conclusion"] != "success":
        raise RuntimeError("Select a completed, successful Actions run")
    expected = (str(run_id), int(run["run_attempt"]), run["head_sha"], component)
    actual = (
        str(report["run_id"]),
        report["attempt"],
        report["revision"],
        report["component"],
    )
    if actual != expected:
        raise RuntimeError(
            "Artifact does not match the selected run, attempt, commit and component"
        )


class API:
    def __init__(self, url, token_file):
        parsed = urllib.parse.urlparse(url)
        if parsed.scheme != "http" or parsed.hostname not in (
            "127.0.0.1",
            "localhost",
            "::1",
        ):
            raise RuntimeError("This local helper requires a loopback HTTP Envy API")
        self.url = url.rstrip("/")
        self.token = Path(token_file).read_text().strip()

    def call(self, method, path, body=None, headers=None):
        req = urllib.request.Request(
            self.url + path,
            method=method,
            data=None if body is None else json.dumps(body).encode(),
            headers={
                "Authorization": "Bearer " + self.token,
                "Content-Type": "application/json",
                **(headers or {}),
            },
        )
        try:
            with urllib.request.urlopen(req, timeout=30) as response:
                data = response.read()
                return json.loads(data) if data else None
        except urllib.error.HTTPError as error:
            data = error.read(65536).decode()
            raise RuntimeError("Envy HTTP %s: %s" % (error.code, data)) from None


def wait(api, cid, target, timeout):
    deadline = time.monotonic() + timeout
    while True:
        current = api.call("GET", "/v1/compositions/" + cid)
        if current["phase"] == target:
            return current
        if target == "ready" and current["phase"] in (
            "failed",
            "destroying",
            "destroyed",
        ):
            raise RuntimeError(
                "Composition %s is %s; inspect it in Envy" % (cid, current["phase"])
            )
        if time.monotonic() >= deadline:
            raise RuntimeError(
                "Timed out waiting for %s; composition %s remains %s"
                % (target, cid, current["phase"])
            )
        time.sleep(2)


def run(args):
    config_path = Path(args.config).resolve()
    config = json.loads(config_path.read_text())

    def local(key):
        return (config_path.parent / config[key]).resolve()

    state = local("state_dir")
    state.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(state, 0o700)
    # Serialize local invocations; expected_generation also fences external updates.
    import fcntl

    with (state / "workflow.lock").open("w") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError(
                "This workflow is already running (including its frontend server)"
            ) from None
        api = API(config["api_url"], local("token_file"))
        session_path = state / "session.json"
        session = json.loads(session_path.read_text()) if session_path.exists() else {}
        # Prevent accidentally reusing saved intent with a different target.
        identity = {
            k: config[k]
            for k in (
                "api_url",
                "project",
                "baseline",
                "repository",
                "github_repository",
                "component",
                "name",
            )
        }
        if session and session["identity"] != identity:
            raise RuntimeError("Configuration target changed; use a separate state_dir")
        if args.destroy:
            if not session.get("composition"):
                raise RuntimeError("No saved composition to destroy")
            cid = session["composition"]
            api.call("DELETE", "/v1/compositions/" + cid)
            result = wait(api, cid, "destroyed", args.timeout)
            save(state / "destroyed.json", result)
            print(json.dumps({"composition": cid, "phase": "destroyed"}), flush=True)
            return
        if not args.run:
            raise RuntimeError("--run is required unless --destroy is used")
        frontend = local("frontend_dir")
        revision = command(["git", "rev-parse", "HEAD"], cwd=frontend).strip()
        if command(["git", "status", "--porcelain"], cwd=frontend).strip():
            raise RuntimeError(
                "Commit or stash frontend changes before binding an exact revision"
            )
        if len(revision) != 40:
            raise RuntimeError("Frontend requires a full Git SHA")
        # Reserve the browser port before creating or updating anything.
        server = None
        if not args.no_serve:
            server = http.server.ThreadingHTTPServer(
                ("127.0.0.1", int(config.get("port", 4174))),
                functools.partial(
                    http.server.SimpleHTTPRequestHandler,
                    directory=str(frontend / "dist"),
                ),
            )
        try:
            if session and not session.get("composition"):
                # Recover the exact accepted intent even if CI has since been rerun.
                recovered = api.call(
                    "POST",
                    "/v1/compositions",
                    session["create_request"],
                    {"Idempotency-Key": session["idempotency_key"]},
                )
                session["composition"] = recovered["id"]
                save(session_path, session)
            repo = config["github_repository"]
            run_info = json.loads(
                command(["gh", "api", "repos/%s/actions/runs/%s" % (repo, args.run)])
            )
            if run_info["conclusion"] != "success":
                raise RuntimeError(
                    "Wait for the selected Actions run to succeed, then retry"
                )
            with tempfile.TemporaryDirectory(dir=state) as download:
                command(
                    [
                        "gh",
                        "run",
                        "download",
                        args.run,
                        "--repo",
                        repo,
                        "--name",
                        "envy-build-report",
                        "--dir",
                        download,
                    ]
                )
                report = json.loads(
                    (Path(download) / "envy-build-report.json").read_text()
                )
            validate_report(report, run_info, args.run, config["component"])
            save(state / ("report-%s-%s.json" % (args.run, report["attempt"])), report)
            project = urllib.parse.quote(config["project"], safe="")
            repository = urllib.parse.quote(config["repository"], safe="")
            reporter = API(config["api_url"], local("report_token_file"))
            build = reporter.call(
                "POST",
                "/v1/projects/%s/repositories/%s/builds" % (project, repository),
                report,
            )
            save(state / "build.json", build)
            current = None
            if session.get("composition"):
                current = api.call("GET", "/v1/compositions/" + session["composition"])
                if current["phase"] in ("destroying", "destroyed"):
                    raise RuntimeError(
                        "Saved composition is deleted; use a new state_dir for a new preview"
                    )
                wanted = update_overrides(current, config["component"], build["id"])
                existing = {
                    k: {
                        ("build_id" if v.get("build_id") else "image"): v.get(
                            "build_id"
                        )
                        or v["image"]
                    }
                    for k, v in current["overrides"].items()
                }
                if wanted != existing:
                    current = api.call(
                        "PATCH",
                        "/v1/compositions/" + current["id"],
                        {
                            "expected_generation": current["generation"],
                            "overrides": wanted,
                        },
                    )
            else:
                # Save an intent key before POST so retries after a crash cannot duplicate creation.
                if not session:
                    import uuid

                    session = {
                        "identity": identity,
                        "idempotency_key": str(uuid.uuid4()),
                        "create_request": {
                            "project": config["project"],
                            "baseline": config["baseline"],
                            "name": config["name"],
                            "overrides": {
                                config["component"]: {"build_id": build["id"]}
                            },
                            "ttl": config.get("ttl", "8h"),
                        },
                    }
                    save(session_path, session)
                current = api.call(
                    "POST",
                    "/v1/compositions",
                    session["create_request"],
                    {"Idempotency-Key": session["idempotency_key"]},
                )
                session["composition"] = current["id"]
                save(session_path, session)
            cid = current["id"]
            print("Waiting for composition " + cid, file=sys.stderr, flush=True)
            current = wait(api, cid, "ready", args.timeout)
            save(state / "composition.json", current)
            # Bindings are immutable across compositions, so namespace the frontend name by composition.
            frontend_name = config.get("frontend_name", "web") + "-" + cid
            path = "/v1/projects/%s/frontend-bindings/%s/%s" % (
                project,
                urllib.parse.quote(frontend_name, safe=""),
                revision,
            )
            api.call(
                "PUT",
                path,
                {"composition": cid, "repository": config["frontend_repository"]},
            )
            receipt = api.call("GET", path + "/resolve")
            save(state / "frontend-receipt.json", receipt)
            # Only public configuration is passed to this example frontend build.
            env = {
                k: v
                for k, v in os.environ.items()
                if k in ("PATH", "HOME", "TMPDIR", "LANG")
            }
            env["VITE_ENVY_API_URL"] = receipt["api_url"].rstrip("/") + "/products"
            command(["npm", "run", "build"], cwd=frontend, env=env)
            result = {
                "composition": cid,
                "generation": current["generation"],
                "build": build["id"],
                "api_url": receipt["api_url"],
                "frontend": frontend_name,
                "revision": revision,
                "expires_at": receipt["expires_at"],
            }
            if server:
                url = "http://localhost:%s" % server.server_port
                api.call(
                    "POST",
                    path + "/deployment",
                    {"expected_version": receipt["binding_version"], "url": url},
                )
                result["frontend_url"] = url
            save(state / "result.json", result)
            print(json.dumps(result), flush=True)
            if server:
                print(
                    "Serving frontend; Ctrl-C stops the server and retains the preview. Use --destroy to clean up.",
                    file=sys.stderr,
                    flush=True,
                )
                server.serve_forever()
        finally:
            if server:
                server.server_close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True)
    parser.add_argument(
        "--run", help="explicit successful Actions run ID (also used for rollback)"
    )
    parser.add_argument(
        "--no-serve",
        action="store_true",
        help="build without starting or publishing the local frontend",
    )
    parser.add_argument(
        "--destroy",
        action="store_true",
        help="destroy the saved composition and wait for cleanup",
    )
    parser.add_argument(
        "--timeout", type=int, default=120, choices=range(1, 601), metavar="SECONDS"
    )
    args = parser.parse_args()
    if args.run and not args.run.isdigit():
        parser.error("--run must be numeric")
    if args.destroy and args.run:
        parser.error("--destroy and --run are mutually exclusive")
    os.umask(0o077)
    try:
        run(args)
    except KeyboardInterrupt:
        pass
    except (RuntimeError, OSError, ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
