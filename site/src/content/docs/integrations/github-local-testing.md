---
title: Local GitHub Testing
description: Test real GitHub webhooks, published builds and PR preview cleanup against a local Envy development cluster.
---

This walkthrough connects a dedicated GitHub test repository to your local Envy
cluster. It exercises real App authentication, Actions builds and Kubernetes
resources—not the dashboard's simulated mode. Use a test App and disposable PR;
publishing images and running Actions may consume registry storage and CI minutes.

## 1. Start the local installation

Complete the [Local Quickstart](/getting-started/quickstart/) and the App permissions
section of [GitHub App & PR Previews](/integrations/github-app/). Install `nginx`
and `cloudflared` on the host for the proxy/tunnel steps below.

Run these commands from the Envy checkout, using the default `envy-dev` cluster:

```sh
export KUBECONFIG="$PWD/.envy/envy-dev/kubeconfig"
export ENVY_API_URL=http://127.0.0.1:8081
export PATH="$PWD/.envy/bin:$PATH"
unset ENVY_API_TOKEN ENVY_API_TOKEN_FILE
kubectl config current-context
envy composition list
curl --fail http://baseline.envy.localhost:8080/
```

Confirm the context is `kind-envy-dev` before applying anything. If you customized
the cluster name or ports, adjust every example accordingly. Keep the API and
baseline reachable while testing. An unhealthy baseline must be fixed before a
GitHub-triggered composition can become ready.

The examples below use project `shop`, source repository ID `backend` and component
`pricing`. Substitute your own registered catalog identifiers consistently; the
default quickstart's demo components are different. Use
[application onboarding](/getting-started/onboarding/) to register your test app.

## 2. Mount local credentials

Keep credentials outside source control in a private directory. Use the scoped
build-credentials JSON described in the [setup guide](/integrations/github-app/).
Do not put the App PEM or Envy's admin credential in Actions.

Attach the App key to the dedicated development cluster:

```sh
export ENVY_GITHUB_APP_ID=123456
export ENVY_GITHUB_APP_PRIVATE_KEY_FILE=/absolute/private/path/app.private-key.pem
chmod 600 "$ENVY_GITHUB_APP_PRIVATE_KEY_FILE"
bash deploy/local/github-app.sh
```

For private registry access and CI reporting, use a dedicated Docker configuration
file with inline `auths`, not desktop credential-helper references:

```sh
export ENVY_REGISTRY_CONFIG_FILE=/absolute/private/path/config.json
export ENVY_BUILD_CREDENTIALS_FILE=/absolute/private/path/build-credentials.json
bash deploy/local/build-access.sh
```

This helper installs credentials for the server and local Kind nodes and restarts
their kubelets; run it only against the disposable development cluster. It needs
registry credentials even if your test image is public. For other installations,
use the Helm secret references in the setup guide instead.

Generate a separate random webhook secret of at least 32 characters and save it
as `/absolute/private/path/webhook-secret`, with no trailing newline. Enter the
same value in the GitHub App's webhook settings. Create the local Kubernetes Secret:

```sh
kubectl -n envy-system create secret generic envy-github-webhook \
  --from-file=webhook-secret=/absolute/private/path/webhook-secret \
  --dry-run=client -o yaml | kubectl apply -f -
```

The App-key helper does not mount the webhook secret. Save this non-secret patch
as `webhook-patch.yaml` in a local scratch directory:

```yaml
spec:
  template:
    spec:
      containers:
        - name: server
          env:
            - name: ENVY_GITHUB_WEBHOOK_SECRET_FILE
              value: /github-webhook/webhook-secret
          volumeMounts:
            - name: github-webhook
              mountPath: /github-webhook
              readOnly: true
      volumes:
        - name: github-webhook
          secret:
            secretName: envy-github-webhook
            defaultMode: 0440
```

Apply it, then wait for the server:

```sh
kubectl -n envy-system patch deployment envy-server --type=strategic \
  --patch-file /absolute/path/to/webhook-patch.yaml
kubectl -n envy-system rollout restart deployment/envy-server
kubectl -n envy-system rollout status deployment/envy-server --timeout=180s
```

After rebuilding/redeploying the local control plane, check that these mounts and
environment settings are still present; reapply local configuration if needed.
Restart the server after credential rotation. Never dump Secret contents into logs.

## 3. Expose only the two required routes

The local API grants administrative access in dev mode. **Never tunnel port 8081
directly.** Instead, run a host-side Nginx proxy that only forwards webhook and
scoped build-report POST requests.

Create a fresh scratch directory and save the following as `nginx.conf` there.
Replace `shop/backend` with your exact registered project/repository IDs. Omit
the build-report location if you are only testing webhook envy.

```nginx
pid nginx.pid;
error_log stderr;
events {}
http {
    access_log off;
    client_max_body_size 2m;
    server {
        listen 127.0.0.1:8087;

        location = /webhooks/github {
            if ($request_method != POST) { return 405; }
            proxy_pass http://127.0.0.1:8081;
        }

        location = /v1/projects/shop/repositories/backend/builds {
            if ($request_method != POST) { return 405; }
            proxy_pass http://127.0.0.1:8081;
        }

        location / { return 404; }
    }
}
```

Run from that scratch directory in a separate terminal:

```sh
nginx -t -p "$PWD/" -c nginx.conf
nginx -p "$PWD/" -c nginx.conf -g 'daemon off;'
```

This example runs Nginx on the host, not inside Docker. It forwards the original
body and request headers, including the signature and Authorization header;
see [Nginx proxy documentation](https://nginx.org/en/docs/http/ngx_http_proxy_module.html).
Envy still validates HMAC and scoped build credentials, even in dev mode.

Before opening the tunnel, check the boundary in another terminal:

```sh
# Expect 404: administrative routes must not be reachable through this proxy.
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8087/v1/compositions
# Expect 401: unsigned webhooks must be rejected.
curl -s -o /dev/null -w '%{http_code}\n' -X POST \
  -H 'Content-Type: application/json' -d '{}' http://127.0.0.1:8087/webhooks/github
# Expect 401: a build report requires its scoped token.
curl -s -o /dev/null -w '%{http_code}\n' -X POST \
  -H 'Content-Type: application/json' -d '{}' \
  http://127.0.0.1:8087/v1/projects/shop/repositories/backend/builds
```

Do not continue if these boundaries fail. A webhook configuration error also needs
fixing before a real envy can succeed.

## 4. Start the HTTPS tunnel and verify envy

In another terminal, keep this running:

```sh
cloudflared tunnel --url http://127.0.0.1:8087
```

Copy the generated HTTPS origin into these two settings:

| Setting                         | Example                                                 |
| ------------------------------- | ------------------------------------------------------- |
| App Webhook URL                 | `https://YOUR-TUNNEL.trycloudflare.com/webhooks/github` |
| Actions variable `ENVY_API_URL` | `https://YOUR-TUNNEL.trycloudflare.com`                 |

Keep your local shell's `ENVY_API_URL` pointing at `http://127.0.0.1:8081` for
administration. Never add admin routes to the proxy to make the dashboard work.
The preview URL can remain local: hosted CI only needs to publish/report builds,
not open the preview application in this test.

Keep SSL verification enabled in GitHub. Under the App's **Advanced → Recent
deliveries**, redeliver a ping and confirm `202`. Confirm the receipt in **Applications →
GitHub**. Repeat the proxy boundary checks against the public origin before CI.

[Cloudflare quick tunnels](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/)
are temporary testing endpoints, not production hosting. If the process stops or
its address changes, update both settings. A webhook-only proxy is insufficient
for hosted Actions reporting.

## 5. Exercise the full PR lifecycle

Discover/register the test repository and install the
[labelled Actions workflow](/integrations/github-app/#4-publish-exact-head-builds-from-actions).
Enable a policy with its workflow ID, mapped components and a short permitted TTL.
Use a same-repository branch, not a fork.

| Test                                         | Expected evidence                                                                                                                                                  |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Open a PR without `envy-preview`             | No owned composition.                                                                                                                                              |
| Add the label                                | App comment shows waiting, then the successful exact-head build creates a preview.                                                                                 |
| Wait for ready and open its URL locally      | Deployed SHA equals the PR head; application responds. Record composition ID, generation, URL and expiry.                                                          |
| Push an observable application change        | Requested SHA advances first; after CI and rollout, deployed SHA advances at the same composition ID/URL, with a newer generation and unchanged expiry.            |
| Fail a build or omit one component report    | Pending reason is visible; no incomplete component set is deployed.                                                                                                |
| Close the PR                                 | Preview stops; owned composition finishes destruction; namespace/workloads and route are removed, not merely hidden in the UI. GitHub deployment becomes inactive. |
| Retry an identical old build report          | It cannot recreate the stopped environment.                                                                                                                        |
| Reopen the labelled PR                       | A fresh lifecycle can create a new composition after cleanup.                                                                                                      |
| Let TTL expire while the PR remains labelled | Resources disappear; later builds do not revive it. Explicit restart produces a new URL/TTL.                                                                       |

Use the local CLI while observing:

```sh
envy pr-preview list --project shop
envy pr-preview get PREVIEW_ID
envy composition get COMPOSITION_ID
envy composition events COMPOSITION_ID
kubectl -n envy-system logs deployment/envy-server --tail=100
```

Check the single App comment and requested/deployed SHA separately. Infrastructure
readiness is not application-test success; a rollout failure must not be treated
as proof that the old version is still healthy. Keep an independently created
control composition during testing and verify PR closure does not remove it.

For runtime failures, inspect composition diagnostics and baseline health. Image
pull errors need registry/node credentials; local ingress/TLS errors need runtime
investigation, not changes to App permissions. Allow for asynchronous processing
and the default five-minute recovery scan when testing missed events.

## 6. Finish safely

Close the disposable PR or explicitly stop its preview, and wait for resource
cleanup **before** stopping the tunnel. Disable the test policy if you do not want
future labels to allocate resources. Remove any independently created test
compositions separately; do not delete a shared baseline as part of PR cleanup.

Stop the foreground tunnel and Nginx processes with Ctrl-C. Deactivate the test
App's webhook or restore its intended stable URL, and remove/update the temporary
Actions API URL so later builds do not target a dead tunnel. Revoke disposable
reporting/registry credentials when no longer needed; retain the App key only in
private storage. No cluster-wide teardown is required to finish this test.
