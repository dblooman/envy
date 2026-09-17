# Authentication, identity, and activity

Envy admits one trusted organization. Every admitted identity can manage the catalog, compositions, recipes, and frontend bindings. There is no role hierarchy or user-management UI. Installation settings remain startup configuration.

## Login modes

| `ENVY_AUTH_MODE` | Behavior |
| --- | --- |
| `dev` | Automatic Admin identity (`local:admin`). No login, cookie, or token required. `make dev` selects this explicitly. |
| `password` | Browser login as `admin`; password defaults to `admin`. |
| `google` | Google sign-in restricted to configured domains and/or email addresses. |
| `token` | Legacy shared/named machine credentials; default outside local tooling. No browser token-entry UI. |
| `proxy` | Existing trusted-proxy identity checks. |
| `none` | Explicit anonymous identity, including behind a gateway that controls access without supplying identity. |

Web requests use the application's origin. Connection settings, browser token entry, and saved server-URL overrides have been removed. Installation and Appearance settings remain, and Demo simulation is separate from authentication.

### Password mode

```sh
export ENVY_AUTH_MODE=password
export ENVY_EXTERNAL_ORIGIN=https://envy.example.com
export ENVY_ADMIN_PASSWORD_FILE=/run/secrets/envy-admin-password
```

Use `ENVY_ADMIN_PASSWORD` instead of the secret file if preferred. With neither setting, the password is `admin`. An explicit environment value or file replaces file-configured credentials. Setting both forms at the same precedence level is an error; explicitly empty environment secrets are rejected.

For authenticated local UI testing, use `ENVY_EXTERNAL_ORIGIN=http://localhost:5173` on the API server and run `make ui-dev`. Vite proxies API, authentication, OAuth, and MCP endpoints without injecting credentials. For CLI/MCP login during authenticated Vite testing, also set `ENVY_API_URL` to the Vite origin so the OAuth resource matches the configured public origin. When using the bundled UI on the API port, use that browser-facing origin instead. HTTP is accepted only for loopback origins in password/Google modes.

### Google mode

Create a Google OAuth **Web application** client and register this redirect URI exactly:

```text
https://envy.example.com/auth/google/callback
```

Configure:

```sh
export ENVY_AUTH_MODE=google
export ENVY_EXTERNAL_ORIGIN=https://envy.example.com
export ENVY_GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
export ENVY_GOOGLE_CLIENT_SECRET_FILE=/run/secrets/google-client-secret
export ENVY_GOOGLE_ALLOWED_DOMAINS=example.com
# Optional additional admitted accounts:
export ENVY_GOOGLE_ALLOWED_EMAILS=colleague@gmail.com
```

`ENVY_GOOGLE_CLIENT_SECRET` is an alternative to the secret file. Domains and emails are comma-separated; at least one nonempty allowlist is required. Domain admission validates Google's Workspace `hd` claim, not an email suffix. Email admission requires a verified email. Identity uses Google's issuer and stable `sub` account identifier.

Google verifies identity; Envy never uses Google access tokens as Envy API/MCP credentials. The server needs outbound HTTPS access to Google's discovery, token, and signing-key endpoints.

### Browser and agent sessions

- Browser sessions expire after seven days. Opaque cookies are HttpOnly, SameSite=Lax, host-only, and Secure under HTTPS with the `__Host-` prefix. Cookie mutations require an exact configured Origin and a CSRF token obtained from `/auth/config`.
- CLI/MCP access tokens last fifteen minutes and refresh automatically for at most thirty days from authorization. Browser consent explicitly grants full installation access for that period. Refresh tokens rotate; reuse revokes the grant and its access tokens.
- Web **Sign out** revokes the current browser session. CLI `auth logout` revokes the locally saved agent grant. Web **Sign out everywhere** revokes all browser sessions and agent grants for the identity.
- Password and auth-mode changes invalidate prior human credentials, including if an old setting is later restored. Google allowlists are checked on every authenticated request and refresh. Restart all replicas with the same authentication configuration when changing settings.
- Invalid explicit bearer credentials never fall back to a browser cookie, proxy identity, or automatic Admin.

Session state lives in PostgreSQL and survives server restarts. Migration `011_authentication.sql` adds isolated authentication records; it does not modify existing application or activity data. Apply migrations before starting the new server version (the existing Helm migration hook does this). OAuth signing material is generated once in PostgreSQL, so no extra session secret needs distributing across replicas. Authentication records include token digests/signatures, identity and grant metadata, and short-lived login transactions; reusable Google tokens are not persisted.

Auth transactions use a database advisory lock for atomic code consumption and refresh rotation across replicas. Expired records are pruned during authentication transactions. Password login and Google login starts are limited to 120 attempts per minute per installation; registration is limited to 30 per minute and 1,000 stored clients. Dynamically registered clients expire after 90 days.

## CLI and MCP

```sh
export ENVY_API_URL=https://envy.example.com
envy auth login
envy auth status
envy composition list
envy auth logout
```

Login opens the browser and uses a temporary loopback callback with PKCE. A URL is also printed to stderr, and login times out after five minutes. Commands keep JSON stdout. Ordinary commands and stdio MCP never open a browser automatically: missing login produces a `envy auth login` instruction. Local dev mode needs no login, even if credentials for a prior mode remain saved.

CLI and stdio MCP share an owner-only, per-origin credential file under the operating system's user-config directory (`envy/credentials`; on macOS, `~/Library/Application Support/envy/credentials`). Atomic writes and a cross-process file lock prevent refresh-token races. Explicit `--token-file` / `ENVY_API_TOKEN_FILE` and then `ENVY_API_TOKEN` override saved login; an invalid or empty explicit token file is an error.

For remote MCP, add **`https://envy.example.com/mcp`** in a client supporting Streamable HTTP and OAuth authorization. The client discovers the issuer, registers, opens browser login/consent, and obtains an Envy token bound to `/mcp`. REST/CLI tokens are bound to `/v1`; neither resource accepts the other's OAuth tokens. Existing named machine credentials can serve unattended MCP integrations, while build credentials remain restricted to build reporting.

Endpoints:

- `GET /auth/config`; `POST /auth/password`, `/auth/logout`, `/auth/logout-all`; `GET /auth/google/start`, `/auth/google/callback`.
- `GET /v1/session` reports the authenticated identity; `GET /v1/installation` reports sanitized settings.
- `GET /.well-known/oauth-authorization-server`; `GET /.well-known/oauth-protected-resource/mcp` and `/v1`.
- `GET|POST /oauth/authorize`; `POST /oauth/token`, `/oauth/register`, `/oauth/revoke`.

Public-client authorization uses S256 PKCE and registered redirect URIs. Dynamic registration and preregistered clients are supported. Optional JSON configuration `auth.oauth_clients` contains `{ "id": "my-client", "name": "My client", "redirect_uris": ["https://client.example/callback"] }` entries. Client-ID metadata-document fetching, device-code login for a separate remote terminal, and AWS load-balancer-specific identity verification are deferred. Remote terminals should use named machine credentials or a local browser/callback tunnel.

## Existing proxy and machine authentication

Token mode requires at least one shared or named machine credential. `ENVY_API_TOKEN_FILE` supplies the shared credential; `ENVY_MACHINE_CREDENTIALS_FILE` is a JSON array of `{ "id": "release-agent", "display_name": "Release agent", "token": "..." }` objects. Machine tokens must be unique and at least 32 characters. Build credentials retain their repository-specific route restrictions.

Proxy mode trusts identity only when the direct peer is within `ENVY_TRUSTED_PROXY_CIDRS` and supplies `X-Envy-Proxy-Secret`, configured through `ENVY_PROXY_SECRET_FILE`. The proxy must remove inbound identity headers and supply one identity. Default headers are `X-Envy-User` and `X-Envy-Email`. Cookie-backed mutations require a same-origin Origin. This release preserves proxy behavior; Envy browser OAuth login is available in password and Google modes.

## Verification

Automated tests exercise password login, a mocked Google issuer and signed ID tokens, cookie/CSRF checks, admission restrictions, persisted sessions, concurrent code exchange, replay revocation, resource binding, logout, and cross-process credential refresh. MCP SDK tests exercise concurrent callers and identity propagation.

A real Google smoke test requires operator-provided Google credentials:

1. Register the exact callback above and configure an allowed domain or email.
2. Sign in from a private browser window; verify the account in Installation and reload to check session persistence.
3. Run `envy auth login` and `envy auth status`; connect a remote MCP client and approve its consent screen.
4. Verify a disallowed account cannot enter, then sign out everywhere and check that browser, CLI, and MCP credentials are rejected.

## Activity

Callers may set `X-Envy-Channel` to `api`, `cli`, `mcp`, `github`, or `web`, and may attach a short `X-Envy-Task` label. These fields describe the caller's workflow and never replace authenticated identity.

Accepted mutations write allowlisted operational activity in the same PostgreSQL transaction as the business change. Composition creates, updates, deletes, and expiry also write an immutable desired-state revision and persist the initiating identity on their durable operation. Idempotency replay does not duplicate activity. Rejected authenticated mutations are recorded separately when PostgreSQL is available. Existing lifecycle events continue to record reconciler observations; older rows have unavailable identity rather than inferred attribution.

Activity is retained by default. Set `ENVY_AUDIT_RETENTION` to a positive Go duration to prune activity and revision snapshots at startup. This is operational history, not tamper-proof compliance storage.

## Configuration file

`ENVY_CONFIG_FILE` may point to a strict JSON startup file. Environment variables take precedence over file values. Secret-file references are supported for passwords and Google client secrets; environment secret values override JSON configuration. Use `admin_password_file`, `google_client_secret_file`, `google_client_id`, `google_allowed_domains`, and `google_allowed_emails` under `auth` for the new modes.

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
