---
title: Local Quickstart
description: One Helm install for Envy, Istio, PostgreSQL, and a working sample application.
---

Install the complete evaluation stack, open the dashboard, and create a real
preview. The quickstart chart includes **Envy, Istio, PostgreSQL, and a tea-shop
application**. No setup script, database configuration, or Envy CLI is needed.

## Prerequisites

- **A dedicated local Kubernetes cluster**, such as Kind or Docker Desktop
  Kubernetes, with `kubectl` pointing at it. Use a cluster without an existing
  Istio installation. See [Kind's quick start](https://kind.sigs.k8s.io/docs/user/quick-start/)
  if you need one.
- **Cluster-admin installation permissions**: the bundled mesh creates
  cluster-wide CRDs and RBAC. Do not use a shared cluster for this quickstart.
- **Helm**: `brew install helm` on macOS installs the standard
  [Homebrew formula](https://formulae.brew.sh/formula/helm).
- **A default storage class** for PostgreSQL's 1 GiB volume. Kind and Docker
  Desktop normally provide one.
- Network access to download the chart and images from Docker Hub.
  Start with 4 CPUs and 8 GiB of Docker memory; initial image downloads can take
  several minutes.

No mesh or database needs to be installed beforehand. For a cluster with existing
infrastructure, use [Install a Release](/getting-started/installation/) instead.

## 1. Install everything

```sh
helm upgrade --install envy oci://registry-1.docker.io/davey/envy-quickstart \
  --version 0.7.0 --namespace envy-quickstart --create-namespace \
  --wait --wait-for-jobs --timeout 10m
```

Use the release name and namespace shown. This intentionally fixed local setup
creates the mesh, database, Envy, and sample application, then registers the
**Tea shop / staging** baseline. Helm waits for the setup Job to finish.
Database credentials are generated automatically.

## 2. Open the dashboard

```sh
kubectl -n envy-quickstart port-forward service/envy-ingress 8080:80
```

Keep that terminal running and open **[http://127.0.0.1:8080](http://127.0.0.1:8080)**.
Sign in with **admin / admin**. Leave Demo simulation off: this installation has
a real sample application.

The same port-forward handles the dashboard and preview traffic.
The baseline is at [http://shop.envy.localhost:8080/products](http://shop.envy.localhost:8080/products);
it shows tea priced at `1200` minor units (£12.00).

## 3. Create a working preview

1. Open **Previews**, choose **Create preview**, and select **Tea shop / staging**.
2. Select the **pricing** component to override.
3. Choose **Direct image** and enter `davey/envy-demo:0.7.0-v2`.
4. Give the preview a name, review the changes, and create it.
5. Wait for readiness, then open the preview URL. Its root redirects to `/products`.

The preview shows a price of **990** (£9.90). Reload the baseline: it still shows
**1200** (£12.00). The storefront is shared; only the preview's pricing service
uses the new image.

When finished, destroy the preview in the dashboard. See the
[web interface walkthrough](/guides/web-interface/) for more controls.

## Local addresses

Use `127.0.0.1:8080` for dashboard login. `localhost` is a different
authentication origin. CLI users can optionally [install the CLI](/reference/cli-installation/)
and set `ENVY_API_URL=http://127.0.0.1:8080`.

Most browsers resolve `*.localhost` to loopback. If your system resolver does
not, use curl's explicit mapping for the baseline or returned preview hostname:

```sh
curl --resolve shop.envy.localhost:8080:127.0.0.1 \
  http://shop.envy.localhost:8080/products
```

Port 8080 must be available on your laptop. Stop another local service using it
before opening the port-forward. No public DNS or certificates are needed.

## Stop or remove the evaluation

Stopping the port-forward closes local access; it does not delete workloads.
Destroy all previews in the dashboard before uninstalling:

```sh
helm uninstall envy --namespace envy-quickstart
```

The PostgreSQL volume and credential Secret are retained so a reinstall can use
the same database. Istio CRDs also remain. For a complete reset, delete the
dedicated local cluster through Kind or Docker Desktop; this deletes its database
contents too. Do not delete a cluster that contains other work.

## Move to your own installation

This chart is for local evaluation, with a default password, plain HTTP, and
a single PostgreSQL pod. When you are ready to configure a shared installation,
follow [Install a Release](/getting-started/installation/) with your managed
database, mesh, authentication, and HTTPS settings.
