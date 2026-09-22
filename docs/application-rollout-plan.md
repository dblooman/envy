# Application preview rollout plan

Use this plan to onboard an HTTP application to Envy without adding
application-specific behavior to the product. Keep deployment details, private
repository references and environment evidence in the installation owner's
configuration repository. Public fixtures should use synthetic applications,
identities, configuration and data.

The generic composite HTTP profile and onboarding readiness helper are
implemented. Real application readiness still depends on its execution controls,
installation prerequisites and business-level acceptance.

## Platform validation with a working-application assumption

Validate Envy's orchestration independently of application implementation. Use a
synthetic HTTP image and supporting containers to exercise the required Pod shape,
startup order, copied configuration, Service routing, DNS, shared dependency
connectivity, readiness, image updates and cleanup. Compiling a private application
is not a prerequisite for this platform acceptance stage.

`make test-composite-e2e` provides this local path in a disposable cluster. Its
second native sidecar proxies requests to a synthetic shared service outside the
preview namespace. The [simulator](composite-simulator.md) checks a read-only
application request, distinct preview releases, dependency loss and recovery, and
isolated preview traffic. HTTP is the fixture protocol; this does not establish
SQL or broker protocol compatibility, cloud identity authorization or business
correctness. Retain those as installation acceptance gates when an environment
becomes available.

## 1. Capture the deployed contract

The application and platform owners should record:

- The exact rendered Deployment, Service selector and port, namespace, ingress
  and mesh configuration, including renderer versions and source revision.
- The selected application, regular sidecars, one-shot init containers and
  native sidecars in their actual startup order.
- Container image digests, probes, security settings and resource requests/limits.
- Configuration dependencies by name and purpose, without Secret payloads.
- Source and preview identities, required external grants and cleanup ownership.
- Shared non-production dependencies and a synthetic read-only business scenario.

Compare rendered manifests with running resources. Fix unsupported resource or
security settings in the source workload or an approved baseline overlay. Do not
weaken Envy's discovery checks to accommodate unknown fields.

Check Pod-template annotations separately from service-account identity
annotations. Discovery rejects arbitrary Pod-template annotations; the current
exception is the supported Linkerd injection configuration. A chart that uses
other mesh injection or proxy-resource annotations needs an approved integration
or a baseline overlay using the installation's namespace-managed injection.
Verify the resulting injection behavior and configure Envy's mesh resource
budgets before approval; simply deleting annotations does not prove equivalent
behavior.

Envy consumes approved Deployments and configuration. The platform remains
responsible for charts, infrastructure controllers, external secret delivery,
baseline migrations and other installation prerequisites.

## 2. Establish application preview behavior

Define explicit controls for operations that can affect shared dependencies:

| Capability | Initial HTTP pilot behavior | Required evidence |
| --- | --- | --- |
| Startup migrations and grants | Disabled; schema prepared externally | Startup, restart and image update perform no migration or grant |
| Seeders and maintenance | Disabled or explicitly isolated | No unsolicited writes or maintenance execution |
| Consumers and publishers | Disabled or connected to an isolated test sink | No shared subscription consumption or publishing, including retries |
| Embedded timers | Disabled | Time and process restarts cause no scheduled business work |
| External commands | Controlled synthetic dependencies | No unintended account, notification or other remote mutation |
| Health checks | Reflect enabled capabilities | Disabled features do not fail readiness; broken required dependencies do |

Prefer explicit registration and execution gates over relying on missing
credentials to stop unwanted work. Test normal baseline behavior as well as
preview behavior. Use synthetic fixtures and externally prepared compatible
schema. Schema-changing previews need a separately provisioned disposable data
store and migration procedure.

Use the [HTTP business acceptance runner](../integrations/onboarding/SMOKE.md)
for configurable baseline/preview JSON assertions, and supplement it with
application-owned tests for behavior outside that contract. A mocked in-process test does
not establish cloud access, mesh propagation or real dependency compatibility.

## 3. Prepare the installation

Install a scoped [composite policy](composite-previews.md) when needed. Identity
annotations alone do not establish external access: verify the approved identity
and grants in generated composition namespaces, together with DNS, networking,
TLS and egress. Define who creates and removes any external grants.

If access requires an external provisioning controller, resolve its readiness
handoff before launching the workload. The current implementation has no cloud
prerequisite controller or external IAM readiness hook. Envy does not provision
cloud accounts, databases or broker resources.

Register the complete baseline catalog and explicitly discover, review and
approve derived profiles. Run the [readiness workflow](../integrations/onboarding/README.md)
to collect catalog checks, approval currency and operator prerequisite evidence.
Keep unknown prerequisites pending. The report is evidence for a creation review,
not proof of live application behavior or an enforcement boundary.

## 4. Prove one HTTP preview, then isolation

Begin with one application image override using a digest-backed build. Verify:

1. Baseline and preview both serve the prepared synthetic business scenario.
2. The preview uses its approved application, supporting execution and identity.
3. Two previews can run distinct images without changing each other or baseline.
4. Updating an application image preserves captured supporting configuration.
5. A required-container failure produces useful readiness and log diagnostics.
6. Explicit deletion and TTL remove owned resources without deleting baseline
   resources; externally managed grants follow their own cleanup contract.

For a distributed scenario, demonstrate that a request through an unchanged
service reaches the intended downstream override. Check context propagation,
selected workload evidence and baseline inheritance independently of HTTP health.
HTTP reachability alone does not prove routing or business correctness.

## 5. Connect build automation

Reuse existing application CI to publish immutable images and report the exact
source revision and digest. Verify that PR builds use the intended head revision,
including reusable workflow inputs and checkout behavior. Validate image pull
access from preview namespaces.

Only enable automatic preview creation after the manual pilot passes. Test new
head updates, close/expiry cleanup and rejection of late reports that would revive
an obsolete preview. Keep repository mappings and credentials outside public
examples.

## Delivery sequence

| Stage | Ownership | Exit criterion |
| --- | --- | --- |
| Deployed contract | Application and platform | Rendered shape, dependencies, identity and scenario have owners and evidence |
| Generic Envy support | Envy | Synthetic composite and derived-preview acceptance pass; implemented |
| Application execution controls | Application | Startup and retries have no unintended effects; baseline behavior remains correct |
| First real preview | Application and platform | Two digest-pinned previews, update, diagnostics and cleanup pass |
| Distributed HTTP proof | Application and platform | Propagation and workload selection demonstrated independently of health |
| CI adoption | Application and platform | Exact-revision builds drive lifecycle with reliable cleanup |

Capture the deployed contract first. Application execution controls and platform
identity/network preparation can then proceed in parallel using that contract.
The first live preview depends on both. CI setup can be prepared in parallel, but
automatic workload creation should follow successful manual acceptance.

Workers, jobs, schedules and broker-specific isolation remain separate product
work. See [workload use cases](workloads.md) for their requirements; a health
endpoint does not turn a background workload into a supported HTTP preview.
