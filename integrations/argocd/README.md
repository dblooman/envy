# Argo CD ownership examples

These manifests are deliberately generic placeholders for the three Argo CD
applications in the Envy integration:

| Application | Owns | Must not own |
| --- | --- | --- |
| `envy-platform` | The long-lived baseline namespace and the shared Istio `Gateway` | Baseline workloads, baseline `VirtualService`, Envy composition resources |
| `envy-baseline` | Baseline Deployments, Services, ServiceAccounts, ConfigMaps, Secrets, NetworkPolicies, and the baseline ingress `VirtualService` | The namespace, shared `Gateway`, Envy composition namespaces, or Envy-generated `VirtualService` objects |
| `envy-control-plane` | The Envy Helm release in `envy-system` and its control-plane RBAC | The baseline, the shared `Gateway`, composition namespaces, workloads, or routes created by Envy |

The platform example owns the `Gateway` even when the object lives in the
baseline namespace. This makes the shared ingress boundary explicit: the
baseline application owns only its exact-host ingress `VirtualService`, and
Envy reads the `Gateway` and attaches its generated preview `VirtualService`
objects to it. Envy never modifies or replaces the `Gateway`.

The platform and baseline files use `example-*` repositories and namespaces so
that they cannot be mistaken for a production installation. Replace those
values, and keep the same repository URL in each corresponding `AppProject`
and `Application`. The Envy application points at this repository's
`deploy/helm/envy` chart; change its `repoURL` if the chart is mirrored or
vendored elsewhere.

## Files

- [`appproject-platform.yaml`](appproject-platform.yaml) and
  [`application-platform.yaml`](application-platform.yaml) — shared ingress
  and baseline namespace.
- [`appproject-baseline.yaml`](appproject-baseline.yaml) and
  [`application-baseline.yaml`](application-baseline.yaml) — long-lived
  application release.
- [`appproject-envy.yaml`](appproject-envy.yaml) and
  [`application-envy.yaml`](application-envy.yaml) — the Envy control-plane
  Helm release.

Apply the three projects before their applications:

```sh
kubectl apply -f integrations/argocd/appproject-platform.yaml
kubectl apply -f integrations/argocd/appproject-baseline.yaml
kubectl apply -f integrations/argocd/appproject-envy.yaml

kubectl apply -f integrations/argocd/application-platform.yaml
kubectl apply -f integrations/argocd/application-baseline.yaml
kubectl apply -f integrations/argocd/application-envy.yaml
```

The examples assume the Argo CD API is in the `argocd` namespace and the
cluster destination is `https://kubernetes.default.svc`. Change the exact
destination server if Argo CD manages a registered external cluster.

## Replace before syncing

1. Change `repoURL`, `targetRevision`, and `path` in every `Application`.
   Each source repository must be listed exactly in its `AppProject`.
2. Replace `example-baseline` with the namespace registered in Envy's catalog.
   The platform application must create that namespace; the baseline
   application must not render a second `Namespace` object.
3. Set the baseline `Gateway` selector, listeners, hosts, and TLS credential
   reference in the platform-owned source. The listener must cover both the
   baseline host and Envy's preview host domain.
4. Make the baseline ingress `VirtualService` an exact-host route that removes
   incoming `baggage` and sends traffic to the baseline entry Service. Do not
   put that `VirtualService` in the platform source.
5. For `application-envy.yaml`, use an environment-specific values file based
   on [`deploy/helm/envy/values-example.yaml`](../../deploy/helm/envy/values-example.yaml).
   Set `installationID`, `externalDatabase`, `runtime`, `auth`, and `istio`
   values for the cluster. Keep database, proxy, machine-credential, and
   image-pull Secret values in operator-managed Secret storage, not in Git.
   The chart is named `envy`; with the example release name `envy`, its
   resources use names such as `envy-envy`.
6. Ensure the Envy control-plane ServiceAccount can reach the existing
   PostgreSQL and ingress endpoints. The chart's RBAC is separate from the
   Argo CD project permissions and remains responsible for composition
   resources.

The platform and baseline sources are intentionally separate. If a team
chooses to keep the `Gateway` in the baseline repository instead, move the
`Gateway` kind/resource scope and ownership together; never render the same
`Gateway` from both applications.

## Destinations and resource scopes

Each project has one exact cluster destination and an API-group/kind allowlist.
The examples do not use `*` for repositories, destinations, or resources:

- `envy-platform` may create only `Namespace` cluster resources and Istio
  `Gateway` resources in `example-baseline`.
- `envy-baseline` may create only the namespaced application kinds listed in
  its project, in `example-baseline`; it has no cluster-resource permission.
- `envy-control-plane` may create its `envy-system` `Namespace`, the two
  cluster RBAC resources emitted by the chart, and the namespaced resources
  emitted by that chart. It has no permission for Istio `Gateway` or
  `VirtualService` resources.

AppProject allowlists constrain API groups and kinds, not individual object
names. Keep the source paths narrow and add a kind only when the rendered
application needs it. Do not broaden a project to `*` to work around a
manifest mistake.

## Shared resources and pruning

- Keep `FailOnSharedResource=true` in all three applications. A sync should
  fail when two applications claim the same object instead of silently
  adopting it.
- The platform application has `prune: false` because its namespace and
  `Gateway` are shared ingress infrastructure. Remove them only through an
  explicit, reviewed operation after draining previews and moving the
  catalog.
- The baseline and control-plane applications use `prune: true` only for
  resources in their own destination and source path. Review deletions when
  changing chart paths or resource kinds.
- Envy-generated composition namespaces, override workloads, aggregate mesh
  `VirtualService` objects, and per-composition preview `VirtualService`
  objects are not Argo desired state. Do not add Argo tracking annotations or
  Git manifests for them, and do not use Argo prune exclusions as a substitute
  for the ownership boundary.
- The platform `Gateway` is shared at runtime but has one Git owner. Envy
  reads it and uses its configured selector/listeners; it does not write the
  `Gateway`. If Argo changes those listeners or hosts, run catalog validation
  before accepting new compositions.

After the baseline application is healthy, run the normal read-only catalog
validation and then apply the catalog. A sync health check is not a catalog
registration: it must not be used to skip Envy's Gateway, Service, sidecar,
and baseline connectivity checks.
