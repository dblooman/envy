---
title: Install a Release
description: Install Envy with Helm on local or remote Kubernetes, then open the dashboard.
---

**Install Envy with Helm, then open the dashboard. You do not need the Envy CLI,
Go, or a source checkout.** The same chart works on Docker Desktop Kubernetes
and remote clusters, and pulls the prebuilt `davey/envy:0.3.0` image.

:::tip[Just want to try Envy on your laptop?]
Use the [Local Quickstart](/getting-started/local-quickstart/) to install Envy,
Istio, PostgreSQL, and a sample app together. Come back here when you want to configure an
installation in your own cluster.
:::

## Before you start

- [ ] A running Kubernetes cluster, locally or remotely, and permission to install workloads and RBAC.
- [ ] `kubectl` configured for that cluster and Helm (`brew install helm` on macOS).
- [ ] PostgreSQL reachable from the cluster, with its connection URL ready.
- [ ] Your chosen Istio, Cilium, or Linkerd mesh installed; see [Choose Your Mesh](/getting-started/mesh-installation/).
- [ ] Network access to Docker Hub to download the released chart and images.

The steps below create the database Secret and Helm values. For the first look,
you can use a local port-forward and password login. Public DNS/TLS, Google
sign-in, and an application baseline can follow before sharing the installation
and creating real previews.

## Install with Helm

With your cluster settings saved in `values.yaml`, the installation command is:

```sh
helm upgrade --install envy oci://registry-1.docker.io/davey/envy-chart \
  --version 0.3.0 --namespace envy-system --create-namespace \
  --values values.yaml --wait --timeout 5m
```

The steps below prepare that file and open the UI. If you only want to see what
Envy looks like, start with the [dashboard walkthrough](/guides/web-interface/).

## 1. Prepare your cluster

You need Helm, `kubectl` access to a Kubernetes cluster, and
PostgreSQL reachable from that cluster. For Docker Desktop, enable Kubernetes
and select its context; Docker alone is not a Kubernetes cluster.

```sh
kubectl config current-context
# For Docker Desktop, if that is your intended cluster:
# kubectl config use-context docker-desktop
```

Envy uses your existing Istio, Cilium, or Linkerd mesh for previews. Follow
[Choose Your Mesh](/getting-started/mesh-installation/) for your cluster's
profile. The chart installs Envy's server, bundled web UI, database migrations,
Service, and permissions. It does not install PostgreSQL or the mesh.

Create the namespace and database Secret before running Helm. Save your actual
PostgreSQL connection URL in a private file named `database-url`, then run:

```sh
kubectl create namespace envy-system
kubectl -n envy-system create secret generic envy-database \
  --from-file=url=./database-url
```

Skip creation if the namespace or Secret already exists. Use the connection URL
and TLS settings supplied by your database operator. Keep this file out of Git.

## 2. Save your settings and install

Save this as `values.yaml` for a first look through a local port-forward on an
**Istio cluster**:

```yaml
installationID: envy-evaluation
externalDatabase:
  secretName: envy-database
  secretKey: url
mesh:
  provider: istio
auth:
  mode: password
  externalOrigin: http://localhost:8081
```

For Cilium or Linkerd, use the [matching mesh settings](/getting-started/mesh-installation/#2-prepare-the-profile-examples)
instead, retaining the evaluation authentication settings above.
Choose the mesh and a stable installation ID before the first install;
they are recorded in the database and cannot be switched on a populated installation.

`values.yaml` is Helm's configuration file. **There is no `installation.json`
requirement and no CLI command to run first.** That separate file is only used
by the optional [installation diagnostic](/reference/cli/#installation-preflight).

Now run the Helm command at the top of this page. It downloads the versioned
chart and starts the released image; there is nothing to build locally.

## 3. Open the dashboard

Once Helm finishes, forward the Envy Service to your laptop:

```sh
kubectl -n envy-system port-forward service/envy-envy 8081:8081
```

Keep that command running and open **[http://localhost:8081](http://localhost:8081)**.
This works for a remote cluster too. The UI and API share this address.
Use `localhost` consistently: the configured authentication origin must match
the browser address, including the port.

With the evaluation settings above, sign in as `admin` with password `admin`.
Set your own password or [Google sign-in](/guides/authentication/) before sharing
the installation. No Vite server or CLI login is needed to use this bundled UI.

Your new installation starts with an empty catalog. You can look around, or
select **Explore demo** on the login screen to view sample previews. Demo
simulation does not deploy workloads or create working preview endpoints.

## 4. Create your first real preview

When you are ready to connect an application:

1. Complete your [mesh and preview ingress settings](/getting-started/mesh-installation/),
   including preview DNS/TLS, and rerun Helm with the updated values.
2. [Configure authentication](/guides/authentication/) and a public HTTPS origin
   if other people will use the installation.
3. [Onboard a running application](/getting-started/onboarding/), then create and
   inspect a preview in the [web interface](/guides/web-interface/).

GitHub automation, agent connections, and [CLI installation](/reference/cli-installation/)
can follow when you need them.

## Upgrades and troubleshooting

Pin the chart version and retain your values file. To upgrade, review the target
release notes, back up PostgreSQL, and rerun the same Helm command with the new
version. The chart selects its matching image automatically.

If Helm times out, check pods and migration Job logs in `envy-system`; a missing
database Secret or unreachable database prevents installation. See the
[installation reference](https://github.com/dblooman/envy/blob/main/docs/installation.md)
for detailed networking and Secret settings. Helm rollback does not reverse
database migrations; see the [operations guide](https://github.com/dblooman/envy/blob/main/docs/operations.md).
