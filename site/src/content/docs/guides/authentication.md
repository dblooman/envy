---
title: Authentication & Google Sign-in
description: Configure Google sign-in, password login, and local development access for the Envy dashboard, CLI, and MCP.
---

Envy uses one identity system for the dashboard, CLI, and MCP. Google verifies who you are; Envy creates its own sessions and agent credentials. Every admitted account has full installation access. There are no roles or user-management screens.

## Choose a login mode

| Mode       | Experience                                                                                       |
| ---------- | ------------------------------------------------------------------------------------------------ |
| `dev`      | Opens directly as Admin, without login. Local `make dev` selects this explicitly.                |
| `password` | Sign in as `admin`. The password defaults to `admin`, with environment or secret-file overrides. |
| `google`   | Sign in with a Google account admitted by your domain or email allowlist.                        |

The default outside local development remains legacy `token` mode. Existing `token`, `proxy`, and `none` configurations remain supported. Browser token entry has been removed; use password or Google mode for interactive login.

## Set up Google sign-in

### 1. Choose the public Envy URL

Use the control-plane URL that users open in their browsers, for example `https://envy.example.com`. This is the dashboard and API origin, not a preview application's URL. Serve it over HTTPS and set the same origin on every Envy replica.

### 2. Create a Google OAuth client

In your Google Cloud project, configure the OAuth consent screen and its audience, then create an OAuth client with application type **Web application**. Follow [Google's OpenID Connect setup instructions](https://developers.google.com/identity/openid-connect/openid-connect) for the current console steps. If the Google app is in testing, make your test accounts eligible to use it.

Register this **Authorized redirect URI**, replacing the origin with your installation's URL:

```text
https://envy.example.com/auth/google/callback
```

The scheme, host, port, and path must match exactly. Copy the client ID and store the client secret in your deployment's secret store. Google's app audience and Envy's allowlist both control admission; configuring one does not replace the other.

### 3. Configure Envy

Set these variables on the API server:

```sh
export ENVY_AUTH_MODE=google
export ENVY_EXTERNAL_ORIGIN=https://envy.example.com
export ENVY_GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
export ENVY_GOOGLE_CLIENT_SECRET_FILE=/run/secrets/google-client-secret
export ENVY_GOOGLE_ALLOWED_DOMAINS=example.com
```

The secret file must contain the client secret and be readable by the server. `ENVY_GOOGLE_CLIENT_SECRET` is an alternative to the file. Explicit environment settings take precedence over JSON configuration; setting both secret forms at the same level is an error.

At least one nonempty allowlist is required:

- `ENVY_GOOGLE_ALLOWED_DOMAINS`: comma-separated Google Workspace domains. Admission checks Google's `hd` claim, not the suffix of an email address.
- `ENVY_GOOGLE_ALLOWED_EMAILS`: comma-separated individual email addresses. Admission requires a verified email, including for personal Google accounts.

An account matching either allowlist is admitted. For individual accounts instead of an entire Workspace domain, configure only the email list:

```sh
export ENVY_GOOGLE_ALLOWED_EMAILS=alice@example.com,bob@gmail.com
```

Restart the server replicas with the updated configuration. The server needs outbound HTTPS access to Google's discovery, token, and signing-key endpoints. Sessions are stored in the existing PostgreSQL database; the Helm migration hook applies the authentication migration during upgrade.

### Helm configuration

Create a Kubernetes Secret in the Envy release's namespace from a local file containing the Google client secret:

```sh
kubectl -n envy-system create secret generic envy-google \
  --from-file=client-secret=/path/to/google-client-secret
```

Merge the following into your installation values, retaining the database, mesh, image, and other installation settings:

```yaml
auth:
  mode: google
  externalOrigin: https://envy.example.com
  googleClientID: your-client-id.apps.googleusercontent.com
  googleClientSecret:
    name: envy-google
    key: client-secret
  googleAllowedDomains: example.com
  googleAllowedEmails: ""
```

Apply the values through your normal Helm upgrade. The Secret must be in the same namespace as the release. These values configure authentication; your ingress still needs to route the public Envy URL to the server.

### Local Google testing

For Vite at `http://localhost:5173`, register this additional Google redirect URI and configure the server with the matching origin:

```text
http://localhost:5173/auth/google/callback
```

```sh
export ENVY_EXTERNAL_ORIGIN=http://localhost:5173
```

Run `make ui-dev` after configuring the API server in Google mode. Vite proxies `/auth`, `/oauth`, discovery, MCP, and API requests to the backend. Use `localhost` consistently: `127.0.0.1` is a different origin. HTTP is supported only on loopback origins for authenticated local testing.

### Verify the complete flow

1. Open Envy in a private browser window and select **Continue with Google**.
2. Sign in with an allowed account. Check the identity under **Installation**, then reload to verify the session persists.
3. Try an account outside both allowlists and verify access is denied.
4. Complete [CLI and MCP login](#cli-and-mcp-login), including the browser approval step.
5. Select **Sign out everywhere** and verify that the browser and saved agent credentials no longer grant access.

Automated tests use a mocked Google issuer. This smoke test needs your own Google client and accounts.

## Password login

Configure the API server:

```sh
export ENVY_AUTH_MODE=password
export ENVY_EXTERNAL_ORIGIN=https://envy.example.com
export ENVY_ADMIN_PASSWORD_FILE=/run/secrets/envy-admin-password
```

Sign in as `admin`. `ENVY_ADMIN_PASSWORD` is an alternative to the file; with neither setting, the password is `admin`. Set your own password for a shared installation. In Helm, use `auth.mode: password` and `auth.adminPasswordSecret` with `name` and `key` pointing to a Secret in the release namespace.

For local password testing, set the server's external origin to `http://localhost:5173` and run `make ui-dev`. `dev` mode automatically opens as Admin and therefore has no login screen. Demo simulation is independent: turn it off under **Installation → Demo** to return to live authentication.

## CLI and MCP login

```sh
export ENVY_API_URL=https://envy.example.com
delivery auth login
delivery auth status
```

Login opens the browser, reuses your Envy browser session, and asks you to approve full installation access for the requesting client. CLI and local stdio MCP share saved credentials. Ordinary commands never launch a browser implicitly. For authenticated Vite testing, use `ENVY_API_URL=http://localhost:5173` to match the configured public origin.

For remote MCP, add `https://envy.example.com/mcp` to a client supporting Streamable HTTP and OAuth. It discovers Envy's authorization endpoints and opens the same login and approval flow. See the [MCP guide](/agents/mcp-server/) for client setup.

Explicit machine token or token-file settings take precedence over saved login. An invalid explicit credential produces an error rather than falling back to your browser identity.

## Sessions and sign-out

- Browser sessions last seven days. **Sign out** ends the current browser session.
- Agent access tokens last fifteen minutes and refresh automatically for an absolute maximum of thirty days. `delivery auth logout` revokes the saved agent grant.
- **Sign out everywhere** revokes all browser sessions and agent grants for your identity.
- Password changes invalidate existing password sessions and grants. Mode changes invalidate incompatible human sessions. Google allowlists are checked again on authenticated requests and refreshes.

## Troubleshooting

| Symptom                                   | Check                                                                                                                                          |
| ----------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| Google reports `redirect_uri_mismatch`    | Register the exact `ENVY_EXTERNAL_ORIGIN` plus `/auth/google/callback`, including the browser's port.                                          |
| Google signs in but Envy denies access    | Check both allowlists. Workspace access requires `hd`; an email suffix alone does not qualify. Check Google's audience/test-user settings too. |
| Envy opens automatically as Admin         | The server is in `dev` mode, or Demo simulation is enabled.                                                                                    |
| Login reports an unexpected API response  | Update the API server and verify that `/auth/config` returns JSON, not the application's HTML page.                                            |
| Login or sign-out fails through a proxy   | Match the exact browser origin and route authentication endpoints to Envy. Preserve cookies and the request's `Origin` header.                 |
| CLI uses an old token after browser login | Check explicit `ENVY_API_TOKEN`, `ENVY_API_TOKEN_FILE`, or `--token-file` settings; these override saved login.                                |
