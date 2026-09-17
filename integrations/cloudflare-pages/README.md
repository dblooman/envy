# Cloudflare Pages build adapter

Install the matching `envy` binary in the build environment and keep this
adapter and a frontend config in the application repository. The config contains
`project`, `frontend`, `api_url_env` and `api_path`; see
[the shop config](../../examples/shop/frontend.envy.json).

Configure secret `ENVY_API_TOKEN` (or a private `ENVY_API_TOKEN_FILE`) and
`ENVY_API_URL` for the Envy control plane. Cloud builds require a remotely reachable
HTTPS control plane and HTTPS preview API. Local kind URLs cannot work remotely.
The build executable must be available on PATH (`ENVY_DELIVERY_BINARY` can select
an absolute envy binary). Set the Pages build command to:

```sh
node integrations/cloudflare-pages/build.mjs --config frontend.envy.json -- npm run build
```

The adapter uses Cloudflare's `CF_PAGES_COMMIT_SHA` and `CF_PAGES_URL`; it resolves
that exact revision with a 60-second deadline before running the existing build.
A missing association can be registered during the wait. On timeout fix the
association/readiness and retry the build. There is no staging fallback.

The child build receives only the resolved URL as the configured public variable;
all `ENVY_*` variables are removed from its environment. This prevents accidental
environment bundling, but is not a sandbox: trusted build code can still access
files and other credentials available to the build account. Use your CI platform's
secret controls. The adapter runs executable arguments directly, without a shell.

After a successful build it records the caller-reported Pages URL using the
resolved binding version. Hosting occurs outside Envy and may fail after the
build. Verify the deployed URL separately and report a frontend check with the
current binding version and the backend generation tested. Application TLS, CORS,
cookies and login redirects must be configured and tested by the application.

## Local example

Create a shop preview overriding pricing, then bind the full committed frontend
SHA to it. Build with these variables (the token stays in an ignored file):

```sh
export ENVY_API_URL=http://127.0.0.1:8081
export ENVY_API_TOKEN_FILE="$PWD/.envy/envy-dev/api-token"
export ENVY_DELIVERY_BINARY="$PWD/.envy/bin/envy"
export ENVY_FRONTEND_REVISION=$(git rev-parse HEAD)
export CF_PAGES_URL=http://localhost:4174
node integrations/cloudflare-pages/build.mjs --config examples/shop/frontend.envy.json -- node examples/shop/frontend/build.mjs
python3 -m http.server 4174 --bind 127.0.0.1 --directory examples/shop/frontend/dist
```

The shop example permits browser reads from exactly `http://localhost:4174`.
Open that URL and inspect `/products` from the resolved composition. This local
HTTP example does not test TLS, login or Cloudflare deployment. Test adapter
boundaries with `node --test integrations/cloudflare-pages/build.test.mjs`.
