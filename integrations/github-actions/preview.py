#!/usr/bin/env python3
"""Create, update, verify, and destroy an Envy preview from GitHub Actions.

The helper intentionally uses only Python's standard library.  It treats the
Envy API as the source of truth: a preview is not considered ready until the
status and public endpoint contracts say so, and a failed or timed-out wait
never destroys the composition implicitly.
"""

import argparse
import json
import math
import os
from pathlib import Path
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


MAX_RESPONSE_BYTES = 2 * 1024 * 1024
MAX_WAIT_SECONDS = 600
DEFAULT_REQUEST_TIMEOUT = 30
DEFAULT_WAIT_SECONDS = 120
DEFAULT_POLL_SECONDS = 2
CATALOG_PAGE_SIZE = 100
CATALOG_ID = re.compile(r"^[a-z][a-z0-9-]{0,61}[a-z0-9]$|^[a-z]$")
BUILD_ID = re.compile(r"^[a-f0-9]{64}$")
IDEMPOTENCY_KEY = re.compile(r"^[\x20-\x7e]{1,128}$")
GO_DURATION = re.compile(
    r"^(?:(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:ns|us|µs|ms|s|m|h))+$"
)


class PreviewError(Exception):
    """An error safe to print in an Actions log."""

    def __init__(self, message, code="error", exit_code=3):
        super().__init__(message)
        self.code = code
        self.exit_code = exit_code


class InputError(PreviewError):
    def __init__(self, message):
        super().__init__(message, code="input", exit_code=2)


class APIError(PreviewError):
    def __init__(self, message, status=None):
        super().__init__(message, code="api", exit_code=3)
        self.status = status


class WaitTimeout(PreviewError):
    def __init__(self, message):
        super().__init__(message, code="timeout", exit_code=5)


class LifecycleFailure(PreviewError):
    def __init__(self, message):
        super().__init__(message, code="lifecycle", exit_code=4)


class CleanupFailure(PreviewError):
    def __init__(self, message):
        super().__init__(message, code="cleanup", exit_code=6)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def _validate_api_url(value):
    if not isinstance(value, str):
        raise InputError(
            "ENVY_API_URL must be an HTTP(S) API URL without credentials, query, or fragment"
        )
    try:
        parsed = urllib.parse.urlsplit(value)
        hostname = parsed.hostname
        parsed.port
    except ValueError:
        raise InputError(
            "ENVY_API_URL must be an HTTP(S) API URL without credentials, query, or fragment"
        ) from None
    if (
        parsed.scheme not in ("http", "https")
        or not parsed.netloc
        or not hostname
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
        or any(char in value for char in "\r\n")
    ):
        raise InputError(
            "ENVY_API_URL must be an HTTP(S) API URL without credentials, query, or fragment"
        )
    return value.rstrip("/")


def _validate_token(value):
    if (
        not isinstance(value, str)
        or not value.strip()
        or any(char in value for char in "\r\n")
    ):
        raise InputError("ENVY_API_TOKEN is required and must be one line")
    if len(value) > 4096:
        raise InputError("ENVY_API_TOKEN is too long")
    return value.strip()


def _catalog_id(value, field):
    if not isinstance(value, str) or not CATALOG_ID.fullmatch(value):
        raise InputError("%s must be a lowercase catalog identifier" % field)
    return value


def _safe_text(value, field, maximum=128):
    if (
        not isinstance(value, str)
        or not value.strip()
        or len(value) > maximum
        or any(char in value for char in "\r\n")
    ):
        raise InputError(
            "%s must be a non-empty single-line value of at most %d characters"
            % (field, maximum)
        )
    return value


def _idempotency_key(value):
    if not isinstance(value, str) or not IDEMPOTENCY_KEY.fullmatch(value):
        raise InputError("idempotency key must be one to 128 printable characters")
    return value


def _duration(value):
    if not isinstance(value, str) or not GO_DURATION.fullmatch(value):
        raise InputError("ttl must be a positive Go duration such as 8h or 30m")
    if not any(char.isdigit() and char != "0" for char in value):
        raise InputError("ttl must be a positive Go duration")
    return value


def _timeout(value, field="timeout"):
    if isinstance(value, bool):
        raise InputError(
            "%s must be between 1 and %d seconds" % (field, MAX_WAIT_SECONDS)
        )
    try:
        number = float(value)
    except (TypeError, ValueError):
        raise InputError(
            "%s must be between 1 and %d seconds" % (field, MAX_WAIT_SECONDS)
        ) from None
    if not math.isfinite(number) or number < 1 or number > MAX_WAIT_SECONDS:
        raise InputError(
            "%s must be between 1 and %d seconds" % (field, MAX_WAIT_SECONDS)
        )
    return number


def _path_segment(value, field):
    """Validate an API path value before quoting it."""
    _safe_text(value, field)
    if "/" in value or "\\" in value or value in (".", ".."):
        raise InputError("%s is not a safe path segment" % field)
    return urllib.parse.quote(value, safe="")


def _endpoint_url(value):
    if not isinstance(value, str) or any(char in value for char in "\r\n"):
        raise APIError("Envy composition endpoints omitted a valid public endpoint")
    try:
        parsed = urllib.parse.urlsplit(value)
    except ValueError:
        raise APIError(
            "Envy composition endpoints omitted a valid public endpoint"
        ) from None
    if (
        parsed.scheme not in ("http", "https")
        or not parsed.netloc
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
    ):
        raise APIError("Envy composition endpoints omitted a valid public endpoint")
    return value


def _read_json_file(filename, description):
    try:
        data = Path(filename).read_bytes()
    except OSError:
        raise InputError("could not read %s" % description) from None
    if len(data) > MAX_RESPONSE_BYTES:
        raise InputError("%s is too large" % description)
    try:
        return json.loads(data)
    except (UnicodeDecodeError, json.JSONDecodeError):
        raise InputError("%s must contain valid JSON" % description) from None


def _validate_overrides(value):
    if not isinstance(value, dict):
        raise InputError("overrides must be a JSON object")
    if len(value) > 3:
        raise InputError("overrides must contain at most three components")
    result = {}
    for component, override in value.items():
        _catalog_id(component, "override component")
        if not isinstance(override, dict):
            raise InputError("each override must be an object")
        if set(override) - {"image", "build_id"}:
            raise InputError("overrides may contain only image or build_id")
        has_image = "image" in override
        has_build = "build_id" in override
        if has_image == has_build:
            raise InputError(
                "each override must contain exactly one of image or build_id"
            )
        if has_image:
            image = override["image"]
            if (
                not isinstance(image, str)
                or not image
                or len(image) > 512
                or image.strip() != image
                or any(char.isspace() for char in image)
            ):
                raise InputError(
                    "override image must be a non-empty value without whitespace"
                )
            result[component] = {"image": image}
        else:
            if not isinstance(override["build_id"], str) or not BUILD_ID.fullmatch(
                override["build_id"]
            ):
                raise InputError(
                    "override build_id must be a 64-character lowercase SHA-256 ID"
                )
            result[component] = {"build_id": override["build_id"]}
    return result


def _validate_catalog_manifest(value, project, baseline):
    if not isinstance(value, dict):
        raise InputError("catalog manifest must be a JSON object")
    if value.get("api_version") != "envy/v1":
        raise InputError("catalog manifest api_version must be envy/v1")
    manifest_project = value.get("project")
    if not isinstance(manifest_project, dict) or manifest_project.get("id") != project:
        raise InputError("catalog manifest project.id must match the selected project")
    if (
        not isinstance(manifest_project.get("name"), str)
        or not manifest_project["name"].strip()
    ):
        raise InputError("catalog manifest project.name is required")
    if not isinstance(value.get("components"), list) or not value["components"]:
        raise InputError("catalog manifest components must be a non-empty array")
    manifest_baseline = value.get("baseline")
    if (
        not isinstance(manifest_baseline, dict)
        or manifest_baseline.get("id") != baseline
    ):
        raise InputError("catalog manifest baseline is required")
    if manifest_baseline.get("project") not in (None, project):
        raise InputError(
            "catalog manifest baseline.project must match the selected project"
        )
    return value


def _as_object(value, description):
    if not isinstance(value, dict):
        raise APIError("Envy API returned an invalid %s response" % description)
    return value


class API:
    """Small authenticated REST client for the preview workflow."""

    def __init__(
        self,
        api_url,
        token,
        request_timeout=DEFAULT_REQUEST_TIMEOUT,
        task="github-actions-preview",
    ):
        self.base_url = _validate_api_url(api_url)
        self.token = _validate_token(token)
        try:
            request_timeout = float(request_timeout)
        except (TypeError, ValueError):
            raise InputError(
                "request timeout must be between 1 and 120 seconds"
            ) from None
        if (
            not math.isfinite(request_timeout)
            or request_timeout < 1
            or request_timeout > 120
        ):
            raise InputError("request timeout must be between 1 and 120 seconds")
        self.request_timeout = request_timeout
        self.task = _safe_text(task, "task", 128)
        self._opener = urllib.request.build_opener(NoRedirect())

    def request(
        self,
        method,
        path,
        body=None,
        idempotency_key=None,
        timeout=None,
        description="request",
    ):
        if (
            not isinstance(path, str)
            or not path.startswith("/")
            or any(char in path for char in "\r\n")
        ):
            raise InputError("invalid API path")
        if timeout is None:
            timeout = self.request_timeout
        try:
            timeout = float(timeout)
        except (TypeError, ValueError):
            raise InputError("request timeout must be positive") from None
        if not math.isfinite(timeout) or timeout <= 0:
            raise InputError("request timeout must be positive")
        data = None
        headers = {
            "Accept": "application/json",
            "Authorization": "Bearer " + self.token,
            "X-Envy-Channel": "github",
            "X-Envy-Task": self.task,
        }
        if body is not None:
            try:
                data = json.dumps(
                    body, allow_nan=False, sort_keys=True, separators=(",", ":")
                ).encode("utf-8")
            except (TypeError, ValueError):
                raise InputError("request body is not JSON serializable") from None
            headers["Content-Type"] = "application/json"
        if idempotency_key is not None:
            headers["Idempotency-Key"] = _idempotency_key(idempotency_key)
        request = urllib.request.Request(
            self.base_url + path, data=data, method=method, headers=headers
        )
        try:
            with self._opener.open(request, timeout=timeout) as response:
                raw = response.read(MAX_RESPONSE_BYTES + 1)
        except urllib.error.HTTPError as error:
            # Do not read or report an upstream body: it can contain credentials
            # or application data, and stable status-only errors are sufficient.
            try:
                error.close()
            except OSError:
                pass
            raise APIError(
                "Envy API rejected %s (HTTP %d)" % (description, error.code), error.code
            ) from None
        except (urllib.error.URLError, TimeoutError, OSError):
            raise APIError("Envy API could not complete %s" % description) from None
        if len(raw) > MAX_RESPONSE_BYTES:
            raise APIError(
                "Envy API returned an oversized response for %s" % description
            )
        if not raw:
            return {}
        try:
            return json.loads(raw)
        except (UnicodeDecodeError, json.JSONDecodeError):
            raise APIError(
                "Envy API returned invalid JSON for %s" % description
            ) from None

    def _page(self, path, description):
        cursor = ""
        seen = set()
        items = []
        while True:
            query = {"limit": str(CATALOG_PAGE_SIZE)}
            if cursor:
                query["after"] = cursor
            page = _as_object(
                self.request(
                    "GET",
                    path + "?" + urllib.parse.urlencode(query),
                    description=description,
                ),
                description,
            )
            page_items = page.get("items")
            if not isinstance(page_items, list):
                raise APIError("Envy API returned an invalid %s page" % description)
            items.extend(page_items)
            next_cursor = page.get("next_cursor", "")
            if next_cursor in (None, ""):
                return items
            if (
                not isinstance(next_cursor, str)
                or len(next_cursor) > 128
                or next_cursor in seen
            ):
                raise APIError("Envy API returned an invalid %s cursor" % description)
            seen.add(next_cursor)
            cursor = next_cursor

    def discover(self, project, baseline):
        """Return the exact registered project and baseline, including revision."""
        _catalog_id(project, "project")
        _catalog_id(baseline, "baseline")
        projects = self._page("/v1/projects", "project catalog")
        project_record = next(
            (
                item
                for item in projects
                if isinstance(item, dict) and item.get("id") == project
            ),
            None,
        )
        if project_record is None:
            raise APIError("Envy project was not found")
        baselines = self._page(
            "/v1/projects/" + _path_segment(project, "project") + "/baselines",
            "baseline catalog",
        )
        baseline_record = next(
            (
                item
                for item in baselines
                if isinstance(item, dict) and item.get("id") == baseline
            ),
            None,
        )
        if baseline_record is None:
            raise APIError("Envy baseline was not found")
        revision = baseline_record.get("revision")
        if (
            not isinstance(revision, str)
            or not revision
            or len(revision) > 128
            or any(char in revision for char in "\r\n")
        ):
            raise APIError("Envy baseline returned an invalid revision")
        return {
            "project": project_record,
            "baseline": baseline_record,
            "project_id": project,
            "baseline_id": baseline,
            "baseline_revision": revision,
        }

    def validate_catalog(self, manifest):
        return _as_object(
            self.request(
                "POST",
                "/v1/catalog/validate",
                body=manifest,
                description="catalog validation",
            ),
            "catalog validation",
        )

    def create(
        self,
        project,
        baseline,
        name,
        overrides,
        baseline_revision,
        ttl="8h",
        idempotency_key=None,
    ):
        _catalog_id(project, "project")
        _catalog_id(baseline, "baseline")
        _safe_text(name, "composition name")
        _safe_text(baseline_revision, "baseline revision")
        _duration(ttl)
        if idempotency_key is None:
            raise InputError("idempotency key is required when creating a preview")
        payload = {
            "project": project,
            "baseline": baseline,
            "name": name,
            "overrides": _validate_overrides(overrides),
            "expected_baseline_revision": baseline_revision,
            "ttl": ttl,
        }
        return _as_object(
            self.request(
                "POST",
                "/v1/compositions",
                body=payload,
                idempotency_key=idempotency_key,
                description="composition creation",
            ),
            "composition creation",
        )

    def get(self, composition_id):
        path = "/v1/compositions/" + _path_segment(composition_id, "composition ID")
        return _as_object(
            self.request("GET", path, description="composition lookup"),
            "composition lookup",
        )

    def update(
        self, composition_id, expected_generation, overrides, idempotency_key=None
    ):
        path = "/v1/compositions/" + _path_segment(composition_id, "composition ID")
        if (
            isinstance(expected_generation, bool)
            or not isinstance(expected_generation, int)
            or expected_generation < 1
        ):
            raise InputError("expected generation must be a positive integer")
        payload = {
            "expected_generation": expected_generation,
            "overrides": _validate_overrides(overrides),
        }
        return _as_object(
            self.request(
                "PATCH",
                path,
                body=payload,
                idempotency_key=idempotency_key,
                description="composition update",
            ),
            "composition update",
        )

    def status(self, composition_id, timeout=None):
        path = (
            "/v1/compositions/"
            + _path_segment(composition_id, "composition ID")
            + "/status"
        )
        return _as_object(
            self.request(
                "GET", path, timeout=timeout, description="composition status"
            ),
            "composition status",
        )

    def wait(
        self,
        composition_id,
        timeout=DEFAULT_WAIT_SECONDS,
        target="ready",
        poll_seconds=DEFAULT_POLL_SECONDS,
        expected_generation=None,
    ):
        """Wait for a terminal target without ever mutating the composition."""
        timeout = _timeout(timeout)
        return self._wait_until(
            composition_id,
            time.monotonic() + timeout,
            target,
            poll_seconds,
            expected_generation,
        )

    def _wait_until(
        self, composition_id, deadline, target, poll_seconds, expected_generation=None
    ):
        if target not in ("ready", "destroyed"):
            raise InputError("wait target must be ready or destroyed")
        if not math.isfinite(poll_seconds) or poll_seconds < 0 or poll_seconds > 60:
            raise InputError("poll interval must be between zero and 60 seconds")
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise WaitTimeout(
                    "timed out waiting for composition %s" % _display_id(composition_id)
                )
            last = self.status(
                composition_id, timeout=min(self.request_timeout, remaining)
            )
            if expected_generation is not None:
                generation = last.get("generation")
                if not isinstance(generation, int) or isinstance(generation, bool):
                    raise APIError("composition status omitted generation")
                if generation > expected_generation:
                    raise LifecycleFailure(
                        "composition was superseded by another update"
                    )
            phase = last.get("phase")
            if not isinstance(phase, str):
                raise APIError("Envy composition status omitted phase")
            if target == "ready":
                if phase == "ready" and (
                    expected_generation is None
                    or (
                        last.get("generation") == expected_generation
                        and last.get("observed_generation") == expected_generation
                    )
                ):
                    return last
                if phase in ("failed", "destroying", "destroyed"):
                    raise LifecycleFailure(
                        "composition reached a terminal failure before becoming ready"
                    )
            elif phase == "destroyed":
                return last
            elif phase in ("failed", "destroyed") and target == "destroyed":
                raise CleanupFailure("composition cleanup reached a terminal failure")
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise WaitTimeout(
                    "timed out waiting for composition %s" % _display_id(composition_id)
                )
            time.sleep(min(float(poll_seconds), remaining))

    def endpoints(self, composition_id):
        path = (
            "/v1/compositions/"
            + _path_segment(composition_id, "composition ID")
            + "/endpoints"
        )
        result = _as_object(
            self.request("GET", path, description="composition endpoints"),
            "composition endpoints",
        )
        public = (
            result.get("endpoints", {}).get("public")
            if isinstance(result.get("endpoints"), dict)
            else None
        )
        if not isinstance(public, dict) or not isinstance(public.get("ready"), bool):
            raise APIError("Envy composition endpoints omitted a valid public endpoint")
        if not public["ready"]:
            raise APIError("Envy composition public endpoint is not ready")
        _endpoint_url(public.get("url"))
        return result

    def destroy(self, composition_id, timeout=DEFAULT_WAIT_SECONDS):
        path = "/v1/compositions/" + _path_segment(composition_id, "composition ID")
        deadline = time.monotonic() + _timeout(timeout)
        try:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise WaitTimeout("timed out waiting for composition cleanup")
            self.request(
                "DELETE",
                path,
                timeout=min(self.request_timeout, remaining),
                description="composition deletion",
            )
            return self._wait_until(
                composition_id, deadline, "destroyed", DEFAULT_POLL_SECONDS
            )
        except PreviewError as error:
            if error.exit_code == 5:
                raise CleanupFailure(str(error)) from None
            if isinstance(error, CleanupFailure):
                raise
            raise CleanupFailure("composition cleanup did not converge") from None


def _display_id(value):
    if isinstance(value, str) and re.fullmatch(r"[A-Za-z0-9-]{1,128}", value):
        return value
    return "selected composition"


def _load_overrides(json_value, filename):
    if (json_value is None) == (filename is None):
        raise InputError("provide exactly one of --overrides-json or --overrides-file")
    if json_value is not None:
        if not isinstance(json_value, str):
            raise InputError("--overrides-json must be a JSON string")
        if len(json_value.encode("utf-8")) > MAX_RESPONSE_BYTES:
            raise InputError("overrides JSON is too large")
        try:
            value = json.loads(json_value)
        except (TypeError, UnicodeDecodeError, json.JSONDecodeError):
            raise InputError("--overrides-json must contain valid JSON") from None
    else:
        value = _read_json_file(filename, "overrides file")
    return _validate_overrides(value)


def _write_state(filename, value):
    if not filename:
        return
    path = Path(filename)
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        temporary = path.with_name(path.name + ".new")
        temporary.write_text(
            json.dumps(value, sort_keys=True, indent=2) + "\n", encoding="utf-8"
        )
        temporary.chmod(0o600)
        temporary.replace(path)
    except (OSError, TypeError, ValueError):
        raise InputError("could not write preview state") from None


def run_preview(args):
    api = API(
        args.api_url or os.environ.get("ENVY_API_URL", ""),
        os.environ.get(args.token_env, ""),
        request_timeout=args.request_timeout,
    )
    project = _catalog_id(args.project, "project")
    baseline = _catalog_id(args.baseline, "baseline")
    overrides = _load_overrides(args.overrides_json, args.overrides_file)
    catalog = None
    if args.catalog_manifest_file:
        catalog = _validate_catalog_manifest(
            _read_json_file(args.catalog_manifest_file, "catalog manifest"),
            project,
            baseline,
        )
    discovery = api.discover(project, baseline)
    if args.expected_baseline_revision:
        _safe_text(args.expected_baseline_revision, "expected baseline revision")
        if args.expected_baseline_revision != discovery["baseline_revision"]:
            raise APIError(
                "selected baseline revision changed; refusing to mutate a preview", 409
            )
    if catalog is not None:
        api.validate_catalog(catalog)
    key = _idempotency_key(args.idempotency_key)
    accepted = None
    if args.mode == "create":
        accepted = api.create(
            project,
            baseline,
            args.name,
            overrides,
            discovery["baseline_revision"],
            ttl=args.ttl,
            idempotency_key=key,
        )
    else:
        composition_id = _safe_text(args.composition_id, "composition ID")
        current = api.get(composition_id)
        if current.get("project") != project or current.get("baseline") != baseline:
            raise InputError(
                "composition does not belong to the selected project and baseline"
            )
        current_revision = current.get("baseline_revision")
        if current_revision != discovery["baseline_revision"]:
            raise APIError(
                "composition baseline revision is no longer current; refusing to update",
                409,
            )
        if args.expected_generation is None:
            raise InputError("expected generation is required in update mode")
        accepted = api.update(
            composition_id, args.expected_generation, overrides, idempotency_key=key
        )
    composition_id = accepted.get("id")
    if not isinstance(composition_id, str) or not composition_id:
        raise APIError("Envy composition response omitted an ID")
    _path_segment(composition_id, "composition ID")
    _write_state(
        args.state_file,
        {
            "composition_id": composition_id,
            "baseline_revision": discovery["baseline_revision"],
            "composition": accepted,
        },
    )
    generation = accepted.get("generation")
    if (
        not isinstance(generation, int)
        or isinstance(generation, bool)
        or generation < 1
    ):
        raise APIError("accepted composition omitted generation")
    status = api.wait(
        composition_id, timeout=args.timeout, expected_generation=generation
    )
    endpoint_response = api.endpoints(composition_id)
    current = api.status(composition_id)
    if (
        current.get("generation") != generation
        or current.get("observed_generation") != generation
        or current.get("phase") != "ready"
    ):
        raise LifecycleFailure("composition changed while resolving its endpoint")
    public = endpoint_response["endpoints"]["public"]
    result = {
        "composition_id": composition_id,
        "generation": status.get("generation", accepted.get("generation")),
        "baseline_revision": discovery["baseline_revision"],
        "preview_url": public["url"],
        "endpoint_ready": public["ready"],
        "composition": accepted,
        "status": status,
        "endpoints": endpoint_response,
    }
    _write_state(args.state_file, result)
    return result


def destroy_preview(args):
    api = API(
        args.api_url or os.environ.get("ENVY_API_URL", ""),
        os.environ.get(args.token_env, ""),
        request_timeout=args.request_timeout,
    )
    destroyed = api.destroy(args.composition_id, timeout=args.timeout)
    result = {
        "composition_id": args.composition_id,
        "phase": destroyed.get("phase"),
        "status": destroyed,
    }
    _write_state(args.state_file, result)
    return result


def _parser():
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    run = subparsers.add_parser(
        "run", help="discover, create/update, wait, and return preview metadata"
    )
    run.add_argument("--api-url", default="", help="Envy API URL (or ENVY_API_URL)")
    run.add_argument(
        "--token-env",
        default="ENVY_API_TOKEN",
        help="environment variable containing the full API token",
    )
    run.add_argument("--project", required=True)
    run.add_argument("--baseline", required=True)
    run.add_argument("--mode", choices=("create", "update"), default="create")
    run.add_argument("--name", default="")
    run.add_argument("--composition-id", default="")
    run.add_argument("--expected-generation", type=int)
    run.add_argument("--expected-baseline-revision", default="")
    run.add_argument("--idempotency-key", required=True)
    run.add_argument("--overrides-json")
    run.add_argument("--overrides-file")
    run.add_argument("--catalog-manifest-file")
    run.add_argument("--ttl", default="8h")
    run.add_argument("--timeout", type=float, default=DEFAULT_WAIT_SECONDS)
    run.add_argument("--request-timeout", type=float, default=DEFAULT_REQUEST_TIMEOUT)
    run.add_argument("--state-file", default="")

    destroy = subparsers.add_parser(
        "destroy", help="delete a composition and wait for its tombstone"
    )
    destroy.add_argument("--api-url", default="", help="Envy API URL (or ENVY_API_URL)")
    destroy.add_argument(
        "--token-env",
        default="ENVY_API_TOKEN",
        help="environment variable containing the full API token",
    )
    destroy.add_argument("--composition-id", required=True)
    destroy.add_argument("--timeout", type=float, default=DEFAULT_WAIT_SECONDS)
    destroy.add_argument(
        "--request-timeout", type=float, default=DEFAULT_REQUEST_TIMEOUT
    )
    destroy.add_argument("--state-file", default="")
    return parser


def main(argv=None):
    args = _parser().parse_args(argv)
    try:
        if args.command == "run":
            if args.mode == "create" and not args.name:
                raise InputError("--name is required in create mode")
            if args.mode == "update" and not args.composition_id:
                raise InputError("--composition-id is required in update mode")
            if args.mode == "update" and args.expected_generation is None:
                raise InputError("--expected-generation is required in update mode")
            result = run_preview(args)
        else:
            result = destroy_preview(args)
        print(json.dumps(result, sort_keys=True, separators=(",", ":")))
        return 0
    except PreviewError as error:
        print(str(error), file=sys.stderr)
        return error.exit_code
    except KeyboardInterrupt:
        print("preview operation cancelled; no cleanup was attempted", file=sys.stderr)
        return 130


if __name__ == "__main__":
    sys.exit(main())
