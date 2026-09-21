#!/usr/bin/env python3
"""Check declared JSON business responses across a baseline and two existing previews."""

import argparse
from datetime import datetime, timezone
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

from readiness import write_report

TARGETS = ("baseline", "preview_a", "preview_b")
ORDER = ("baseline", "preview_a", "baseline", "preview_b", "baseline")
MAX_BYTES = 1024 * 1024


class CheckError(Exception):
    """Only fixed, non-sensitive error codes may enter the public report."""


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        fp.close()
        raise CheckError("redirect_refused")


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("Duplicate JSON object key")
        result[key] = value
    return result


def strict_json(data):
    value = json.loads(
        data,
        object_pairs_hook=unique_object,
        parse_constant=lambda _: reject_constant(),
    )
    canonical(value)
    return value


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False)


def pointer_parts(pointer):
    if (
        not isinstance(pointer, str)
        or len(pointer) > 512
        or (pointer and not pointer.startswith("/"))
    ):
        raise ValueError("Assertions require JSON pointers of at most 512 characters")
    if re.search(r"~(?![01])", pointer):
        raise ValueError("JSON pointer escapes must be ~0 or ~1")
    return (
        [p.replace("~1", "/").replace("~0", "~") for p in pointer[1:].split("/")]
        if pointer
        else []
    )


def select(document, pointer):
    for part in pointer_parts(pointer):
        if isinstance(document, dict) and part in document:
            document = document[part]
        elif isinstance(document, list) and re.fullmatch(r"0|[1-9][0-9]*", part):
            try:
                document = document[int(part)]
            except (IndexError, ValueError):
                raise CheckError("missing_pointer") from None
        else:
            raise CheckError("missing_pointer")
    return document


def validate_url(url):
    if not isinstance(url, str) or any(
        char.isspace() or ord(char) < 32 for char in url
    ):
        raise ValueError("Target URL must be a string without whitespace")
    parsed = urllib.parse.urlsplit(url)
    if (
        parsed.scheme not in ("https", "http")
        or not parsed.hostname
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
        or (
            parsed.scheme == "http"
            and parsed.hostname not in ("localhost", "127.0.0.1", "::1")
        )
    ):
        raise ValueError(
            "Use HTTPS or loopback HTTP target URLs without credentials, queries or fragments"
        )
    _ = parsed.port


def load_config(path):
    raw = path.read_bytes()
    if len(raw) > MAX_BYTES:
        raise ValueError("Smoke configuration exceeds 1 MiB")
    config = strict_json(raw)
    required = {"api_version", "scenario", "targets", "comparisons"}
    if (
        not isinstance(config, dict)
        or not required <= set(config)
        or set(config) - required - {"rounds", "timeout_seconds"}
    ):
        raise ValueError(
            "Expected scenario, targets, comparisons and optional rounds/timeout_seconds"
        )
    if config["api_version"] != "envy-http-smoke/v1":
        raise ValueError("Expected envy-http-smoke/v1")
    if not isinstance(config["scenario"], str) or not re.fullmatch(
        r"[a-z][a-z0-9-]{0,62}", config["scenario"]
    ):
        raise ValueError("Scenario must be a lowercase identifier")
    for key, default, maximum in (("rounds", 2, 10), ("timeout_seconds", 10, 30)):
        value = config.get(key, default)
        if type(value) is not int or not 1 <= value <= maximum:
            raise ValueError(f"{key} must be an integer from 1 to {maximum}")
        config[key] = value
    if not isinstance(config["targets"], dict) or set(config["targets"]) != set(
        TARGETS
    ):
        raise ValueError("Targets must be baseline, preview_a and preview_b")
    routes = set()
    for name, target in config["targets"].items():
        if (
            not isinstance(target, dict)
            or not {"url", "expect"} <= set(target)
            or set(target) - {"url", "expect", "composition_id", "token_file"}
        ):
            raise ValueError(
                "Each target requires url and expect, with optional composition_id/token_file"
            )
        validate_url(target["url"])
        composition = target.get("composition_id", "")
        if (
            not isinstance(composition, str)
            or (composition and not re.fullmatch(r"[a-z0-9-]{1,64}", composition))
            or (name == "baseline" and composition)
        ):
            raise ValueError("Only previews may specify a valid composition_id")
        route = (target["url"], composition)
        if route in routes:
            raise ValueError("Each target must have a distinct URL/composition pair")
        routes.add(route)
        if "token_file" in target and (
            not isinstance(target["token_file"], str)
            or not target["token_file"].strip()
        ):
            raise ValueError("token_file must be a nonempty path")
        expected = target["expect"]
        if not isinstance(expected, dict) or not 1 <= len(expected) <= 32:
            raise ValueError("Each target requires one to 32 JSON pointer expectations")
        for pointer, value in expected.items():
            pointer_parts(pointer)
            canonical(value)
    comparisons = config["comparisons"]
    if not isinstance(comparisons, list) or not 1 <= len(comparisons) <= 32:
        raise ValueError("Declare one to 32 cross-target comparisons")
    for comparison in comparisons:
        if (
            not isinstance(comparison, dict)
            or not {"pointer", "relation"} <= set(comparison)
            or set(comparison) - {"pointer", "relation", "targets"}
            or comparison["relation"] not in ("equal", "distinct")
        ):
            raise ValueError("Comparisons require pointer and relation equal/distinct")
        pointer_parts(comparison["pointer"])
        names = comparison.get("targets", list(TARGETS))
        if (
            not isinstance(names, list)
            or not 2 <= len(names) <= 3
            or not all(isinstance(name, str) and name in TARGETS for name in names)
            or len(set(names)) != len(names)
        ):
            raise ValueError(
                "Comparison targets must name two or three distinct configured targets"
            )
    return config


def tokens_for(config, directory):
    tokens = {}
    for name, target in config["targets"].items():
        token = (
            (directory / target["token_file"]).read_text().strip()
            if "token_file" in target
            else ""
        )
        if "token_file" in target and (
            not token or any(ord(c) < 33 or ord(c) > 126 for c in token)
        ):
            raise ValueError("Token files must contain a nonempty ASCII bearer token")
        tokens[name] = token
    return tokens


def fetch(target, token, timeout):
    headers = {"Accept": "application/json", "Cache-Control": "no-cache"}
    if token:
        headers["Authorization"] = "Bearer " + token
    if target.get("composition_id"):
        headers["baggage"] = "composition=" + target["composition_id"]
    request = urllib.request.Request(target["url"], headers=headers, method="GET")
    opener = urllib.request.build_opener(NoRedirect())
    try:
        with opener.open(request, timeout=timeout) as response:
            if response.status != 200:
                raise CheckError("unexpected_status")
            content_type = response.headers.get_content_type()
            if content_type != "application/json" and not content_type.endswith(
                "+json"
            ):
                raise CheckError("non_json_content_type")
            deadline = time.monotonic() + timeout
            chunks, size = [], 0
            while True:
                if time.monotonic() >= deadline:
                    raise CheckError("response_timeout")
                chunk = response.read1(min(65536, MAX_BYTES + 1 - size))
                size += len(chunk)
                if size > MAX_BYTES:
                    raise CheckError("response_too_large")
                if not chunk:
                    if response.length not in (None, 0):
                        raise CheckError("incomplete_response")
                    break
                chunks.append(chunk)
            return strict_json(b"".join(chunks))
    except urllib.error.HTTPError as exc:
        exc.close()
        raise CheckError(
            "redirect_refused" if 300 <= exc.code < 400 else "unexpected_status"
        ) from None
    except (urllib.error.URLError, OSError, http.client.HTTPException):
        raise CheckError("transport_error") from None
    except (ValueError, UnicodeError, RecursionError):
        raise CheckError("invalid_json") from None


def reject_constant():
    raise ValueError("Non-finite JSON number")


def compare(documents, comparisons):
    results = []
    for index, rule in enumerate(comparisons):
        try:
            values = [
                canonical(select(documents[name], rule["pointer"]))
                for name in rule.get("targets", TARGETS)
            ]
            passed = len(set(values)) == (
                1 if rule["relation"] == "equal" else len(values)
            )
            error = "" if passed else "comparison_mismatch"
        except (CheckError, KeyError):
            passed, error = False, "comparison_unavailable"
        results.append({"assertion": index + 1, "passed": passed, "error": error})
    return results


def run(config, tokens, request=fetch):
    report = {
        "api_version": "envy-http-smoke-report/v1",
        "scenario": config["scenario"],
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "configuration_sha256": hashlib.sha256(
            json.dumps(config, separators=(",", ":"), allow_nan=False).encode()
        ).hexdigest(),
        "passed": True,
        "rounds": [],
        "limitations": [
            "Only the configured HTTP response assertions were checked.",
            "This is not proof of cloud IAM, data isolation, disabled background work or all downstream routing.",
            "No previews were created, updated or destroyed; GET endpoints must be chosen to avoid side effects.",
        ],
    }
    for number in range(1, config["rounds"] + 1):
        documents, observations = {}, []
        for name in ORDER:
            target = config["targets"][name]
            observation = {
                "target": name,
                "passed": False,
                "error": "",
                "failed_assertions": [],
            }
            try:
                document = request(target, tokens[name], config["timeout_seconds"])
                for index, (pointer, expected) in enumerate(target["expect"].items()):
                    try:
                        matched = canonical(select(document, pointer)) == canonical(
                            expected
                        )
                    except CheckError:
                        matched = False
                    if not matched:
                        observation["failed_assertions"].append(index + 1)
                observation["passed"] = not observation["failed_assertions"]
                observation["error"] = (
                    "" if observation["passed"] else "expectation_mismatch"
                )
                documents[name] = document
            except CheckError as exc:
                observation["error"] = str(exc)
                documents.pop(name, None)
            observations.append(observation)
        comparisons = compare(documents, config["comparisons"])
        passed = all(o["passed"] for o in observations) and all(
            c["passed"] for c in comparisons
        )
        report["rounds"].append(
            {
                "round": number,
                "passed": passed,
                "requests": observations,
                "comparisons": comparisons,
            }
        )
        report["passed"] = report["passed"] and passed
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--check-config", action="store_true")
    args = parser.parse_args()
    try:
        config = load_config(args.config)
        if args.check_config:
            print("Configuration valid; no HTTP requests performed")
            return 0
        if args.output:
            protected = [args.config.resolve()] + [
                (args.config.parent / t["token_file"]).resolve()
                for t in config["targets"].values()
                if "token_file" in t
            ]
            if args.output.resolve() in protected:
                raise ValueError("Output must not replace configuration or token files")
        tokens = tokens_for(config, args.config.parent)
        report = run(config, tokens)
        if args.output:
            write_report(args.output, report)
        else:
            print(json.dumps(report, indent=2))
        return 0 if report["passed"] else 2
    except (ValueError, TypeError, KeyError, OSError, RecursionError):
        print(
            "Invalid smoke input or report destination; check schema, URLs and token files",
            file=sys.stderr,
        )
        return 1


if __name__ == "__main__":
    sys.exit(main())
