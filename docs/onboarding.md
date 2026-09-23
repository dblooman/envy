# Application onboarding

For Istio, Cilium, and Linkerd prerequisites, examples, and acceptance status, see
[mesh installation profiles](mesh-installation.md). Istio-specific instructions below apply only to the Istio profile.

A checked-in `envy/v1` JSON configuration describes one project, its complete
baseline component catalog, and one existing baseline. It contains approved
literal workload configuration, never credentials. Envy borrows existing Services
and deployments; operators still install the application and its mesh ingress.

`envy catalog validate --file application.json` performs read-only catalog,
Kubernetes readiness, provider-specific mesh participation, routing ownership and ingress checks. `envy catalog apply --file
application.json` repeats validation and registers the bundle in one PostgreSQL
transaction. Identical existing entries are retained; changed immutable entries
conflict. Repeating apply is safe, including after an uncertain response.

The REST API owns both operations at `/v1/catalog/validate` and
`/v1/catalog/apply`. CLI output is JSON with the normalized configuration,
checks and limitations. No local kubeconfig is required by the CLI.

Verification has two explicit levels:

- `envy-chain`: proves the registered chain, context propagation, selected pod
  identities and live baseline inheritance. Successful compositions report
  `verification_level: routing` and `RouteVerified: true`.
- `http`: checks a configured absolute path and expected successful HTTP status
  through both baseline and preview ingress. Response content is application
  owned. Successful compositions report `verification_level: reachability`,
  `IngressReachable: true`, and `RouteVerified: false`. Healthy pods plus a 200
  response cannot prove downstream propagation, override selection or inheritance.

For HTTP verification, ready means all selected workloads are ready, routing
configuration was accepted, and both ingress probes succeeded. Integrators must
verify propagation and selected workload behavior through their own application
checks. Failed or pending compositions report no current verification evidence.
Resource and access isolation guarantees remain unchanged.

The shop example uses business JSON and ordinary HTTP response headers rather
than Envy's chain schema. Its external acceptance checks independently prove
context propagation and workload selection. Capacity acceptance creates twenty
compositions, checks the twenty-first is rejected, interleaves baseline and
preview requests, measures readiness/latency, and verifies complete cleanup.
This is a development capacity check, not a production throughput benchmark.

For existing Deployments with environment references and configuration mounts,
use the opt-in [deployment-derived workflow](deployment-derived-previews.md).
It adds a discover/review/approve step after baseline catalog registration and
avoids maintaining a separate literal workload configuration.

## Readiness evidence before the first real preview

Use the [generic readiness workflow](../integrations/onboarding/README.md) to
collect catalog/discovery/approval checks and owner-supplied prerequisite evidence.
It reports unresolved work without registering, approving or creating resources.
Cloud access and application behavior still require separate live acceptance.

## Guided dashboard onboarding

In a connected dashboard, choose **Catalog & baselines → Onboard application**.
The flow is intended for a platform engineer who knows the existing Services and
ingress and can arrange required permissions. Discovery inspects named,
approved resources; it does not offer unrestricted cluster browsing or install
infrastructure.

1. Describe the project and complete baseline: Service names and ports, baseline
   images, namespace, Gateway, entry component, and verification settings. HTTP
   verification defaults to `/` and status `200`; select an application path
   that returns the expected status. Instrumented applications can select an
   ordered Envy chain instead.
2. Choose each workload profile. Deployment-derived profiles obtain configuration
   during preparation. Manual `http-small` profiles collect health/readiness
   paths, literal environment settings, and approved registry Secret names.
   Never enter credentials in catalog-visible values.
3. Validate, review the returned checks, and explicitly **Register baseline**.
   This atomically saves immutable catalog entries without creating a preview.
   Matching entries are reused; conflicting changes require correcting the
   submitted configuration, not overwriting the saved catalog.
4. Prepare deployment-derived services: discover configuration, review blockers
   and named source-read rules, arrange permissions externally, and retry.
   Enter supported literal environment or ConfigMap-key replacements as needed.
   Confirm connectivity and shared side effects, then approve the inspected
   configuration. Edits or source/revision conflicts require fresh discovery and
   another review. Manual profiles need no additional preparation.
5. Continue to the existing preview creation screen with the baseline selected.
   Choose a published build or image and review the lifetime before **Create
   preview**. Deployment-derived overrides require an approved profile and an
   immutable image digest or published build. The request guards the reviewed
   profile revisions. Pending services can stay inherited.

During the first two steps, **Save preparation** persists a non-secret draft;
**Load saved preparation** resumes it after a reload. The draft is scoped to
installation, project and authenticated author. Shared-token and anonymous
access use a common author. Environment and selected ConfigMap values, credentials
and messaging configuration are excluded; re-enter them before validation.
Saving with an outdated revision returns 409. A draft neither registers a
baseline nor approves a profile. **Discard saved preparation** removes it.

Saved baselines can be reopened through **Prepare overrides** on their Catalog
card. Registration and completed approvals survive; unsaved edits are discarded
when leaving onboarding. **Advanced registration** retains the JSON editor.
Demo mode cannot onboard a real application.

The preview creation flow requests a read-only server plan showing selected and
inherited components, approved revisions, declared dependencies, destination,
lifetime, upper-bound resource demand, blockers and next actions. The plan is
not a reservation or approval; creation checks the current contract and capacity
again. REST, CLI and MCP expose the same planning semantics.

Read the preview's actual verification level after creation: HTTP reachability
is not proof of downstream routing. Run application checks to establish context
propagation and the selected workloads.
