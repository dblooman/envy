# Authentication, identity, and activity

Envy admits one trusted organization. Every admitted identity can read and manage the catalog, compositions, recipes, and frontend bindings. Installation settings and credentials remain startup configuration; there is no application role hierarchy.

Set `ENVY_AUTH_MODE` to `token` (the compatible default), `proxy`, or `none`. Token mode accepts the existing `ENVY_API_TOKEN_FILE` shared credential and named machine credentials from `ENVY_MACHINE_CREDENTIALS_FILE`. The machine file is a JSON array of `{"id":"release-agent","display_name":"Release agent","token":"..."}` objects. Tokens must be unique and at least 32 characters. The existing build-credential file remains restricted to its repository build-report route.

Proxy mode trusts identity only when the direct peer is within `ENVY_TRUSTED_PROXY_CIDRS` and supplies the secret in `X-Envy-Proxy-Secret`. Configure that secret through `ENVY_PROXY_SECRET_FILE`. The proxy must remove inbound identity headers and set exactly one identity value. Header names default to `X-Envy-User` and `X-Envy-Email` and are configurable. Cookie-backed mutations must carry a same-origin `Origin`. Invalid bearer credentials never fall through to proxy or anonymous identity.

`none` is an explicit no-user mode and records `anonymous` activity. It can sit behind a gateway that controls access without supplying user identity. `GET /v1/session` reports the effective principal and `GET /v1/installation` reports sanitized limits and authentication mode; neither endpoint returns secrets or filesystem paths.

Callers may set `X-Envy-Channel` to `api`, `cli`, `mcp`, `github`, or `web`, and may attach a short `X-Envy-Task` label. These fields describe the caller's workflow and never replace authenticated identity.

Accepted mutations write allowlisted operational activity in the same PostgreSQL transaction as the business change. Composition creates, updates, deletes, and expiry also write an immutable desired-state revision and persist the initiating identity on their durable operation. Idempotency replay does not duplicate activity. Rejected authenticated mutations are recorded separately when PostgreSQL is available. Existing lifecycle events continue to record reconciler observations; older rows have unavailable identity rather than inferred attribution.

Activity is retained by default. Set `ENVY_AUDIT_RETENTION` to a positive Go duration to prune activity and revision snapshots at startup. This is operational history, not tamper-proof compliance storage.

## Configuration file

`ENVY_CONFIG_FILE` may point to a strict JSON startup file. Environment variables take precedence over file values. Secrets are referenced by file path rather than embedded in the configuration.

```json
{
  "installation_id": "envy-dev",
  "listen_addr": ":8081",
  "web_dir": "/web",
  "auth": {
    "mode": "proxy",
    "proxy_secret_file": "/secrets/proxy-secret",
    "machine_credentials_file": "/secrets/machines.json",
    "trusted_proxy_cidrs": "10.0.0.0/8"
  },
  "limits": {
    "default_ttl": "8h",
    "max_ttl": "24h",
    "max_compositions": "20",
    "audit_retention": "2160h"
  },
  "github": {
    "app_id": "12345",
    "private_key_file": "/secrets/github.pem",
    "build_credentials_file": "/secrets/builds.json"
  }
}
```

The container image builds the React application and serves it from the control-plane origin. Unknown frontend paths return the SPA entry point; unknown `/v1` paths retain the API 404 contract.
