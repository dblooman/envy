# Composite HTTP previews

`deployment-composite` derives a preview from an approved Deployment containing
one selected application, named supporting containers, and optional init
containers. The application does not have to be first. Only its image changes
when updating a preview; supporting images and execution settings belong to the
approved contract. Application names, cloud identities and shared dependencies
are installation configuration, not built into Envy.

## Installation policy

Enable dependency-copy permissions and provide a policy for each exact
`project/baseline/component`. Example Envy Helm values:

```yaml
preview:
  enabled: true
  maxCPU: "2"
  maxMemory: 2Gi
  meshRequestCPU: 100m
  meshRequestMemory: 128Mi
  meshLimitCPU: "2"
  meshLimitMemory: 1Gi
  composite:
    shop/staging/pricing:
      revision: 1
      application_container: application
      sidecars: [proxy]
      init_containers: [bootstrap]
      native_sidecars: [database-proxy]
      source_service_account: baseline-pricing
      service_account: preview-pricing
      service_account_annotations:
        identity.example.com/account: preview-nonproduction
      shared_dependencies:
        - Shared non-production database; schema prepared externally
        - Messaging and scheduled application work disabled for this pilot
      max_pod_cpu: "6"
      max_pod_memory: 4Gi
```

The annotation is illustrative, not a cloud identity integration. JSON server
configuration accepts the same policy under `preview.composite`; other preview
fields use the existing snake-case server names, such as `max_cpu`. Restart the
server after changing installation policy.

The three container lists are distinct and exact:

- `sidecars`: regular containers running alongside the application.
- `init_containers`: one-shot containers that must finish successfully.
- `native_sidecars`: init containers with `restartPolicy: Always`, continuing
  alongside subsequent init steps and the application.

The cluster must support restartable init containers when `native_sidecars` are
used. All listed containers must exist; extras are rejected. Platform mesh injection
remains installation-owned and is not included in these lists. At most sixteen
containers and 64 scoped policies are supported. Supporting images must use
immutable `@sha256:` references. Application overrides require an immutable image
or a registered digest-backed build.

Every declared container must explicitly prohibit privilege escalation, run as
non-root, and declare positive CPU/memory requests and limits within installation
maxima. The application, regular sidecars and native sidecars require readiness
probes. One-shot init containers cannot declare probes. Host access, persistent
storage, unsupported scheduling/Pod fields, arbitrary Pod annotations, container
lifecycle hooks and resource claims remain unsupported. Blockers identify the
container/field instead of silently relaxing its security or resources.

`max_pod_cpu` and `max_pod_memory` bound effective declared workload limits,
excluding the separately configured injected mesh. Requirements include regular
containers, persistent native sidecars and the peak of each ordered init step
plus already started native sidecars. Quota adds captured mesh budgets and room
for two Pods per overridden component during rolling updates. Configure mesh
budgets to cover the installation's actual injection.

## Identity and external effects

The source account must match `source_service_account` exactly; an omitted source
account is treated as `default`. Envy creates the separate destination account
with approved annotations, ownership metadata and token automount disabled.
Source annotations and account credentials are not copied. Destination names must
not be `default` or use the reserved `envy-` prefix. Components in one composition
must use distinct destination accounts. Owned metadata drift is repaired; foreign
ownership is rejected.

The complete policy and revision are captured with the snapshot and approval
fingerprint. Changing any captured policy field or removing its installed policy
blocks reconciliation of desired composite workloads before resource writes.
Historical snapshots of removed overrides do not block retirement or remaining
workloads. Existing Pods are not forcibly terminated by revocation. Reapprove and recreate to adopt changed
policy; image updates retain captured configuration. Deletion remains available.

Cloud authorization is operator-managed. An annotation does not establish IAM,
database grants or metadata connectivity in a new namespace. This implementation
does not include a cloud prerequisite controller or external IAM readiness hook.
Prepare access for generated namespaces before enabling a workload; otherwise
startup can fail. Do not supply a production account as a preview identity.

`shared_dependencies` is required human-readable disclosure, not provisioning or
a technical isolation boundary. If there are no external dependencies, state
that explicitly. Never include credentials. Applications must independently
disable or isolate startup migrations, consumers, publishers, seeders and
embedded schedulers, including inherited handlers processing preview requests.
Envy does not replay source charts or execute their Jobs, CronJobs or cloud setup.

## Onboarding and use

1. Prepare a ready baseline, mesh participation and an HTTP Service. Register
   `profile: deployment-composite`, `protocol: http` and the **Service** port.
   Omit manual environment/probe/pull-secret settings. Install the scoped policy
   before baseline validation.
2. Discover using `envy preview-profile discover`, REST/MCP or the dashboard.
   The selected app must match installation policy.
3. Grant returned named source `get` permissions, resolve blockers, and repeat
   discovery. Dependency references in every container are included.
4. Review shared effects and namespace-sensitive addresses. Existing literal env
   replacements apply to the application only; ConfigMap text-key replacements
   work for approved dependencies used by any container. Changes to supporting
   environment/commands require source changes and renewed approval.
5. Approve the returned inspection with connectivity confirmation and expected
   revision. Review all supporting containers and identity metadata.
6. Create with an immutable image/build and optionally guard the approved profile
   revision. Existing update, wait, destroy and TTL behavior applies.

Example after discovery and approval:

```sh
envy composition create --project shop --baseline staging --name pricing-check \
  --override pricing=registry.example/pricing@sha256:REPLACE_WITH_64_HEX_DIGEST \
  --expected-preview-revision pricing=1 --ttl 1h
```

See [deployment-derived onboarding](deployment-derived-previews.md) for approval
payloads, source RBAC and immutable dependency-copy guarantees. Existing
single-container profiles and stored snapshots remain compatible.

## Diagnostics

Readiness retains rollout, endpoints, selected application image and mesh checks,
plus supporting readiness and init completion. A captured execution fingerprint
detects Deployment template changes, including extra containers and altered init
order. Unexpected execution drift blocks readiness/reconciliation and requires
inspection and recreation. Waiting, crash and image-pull
failures identify the container. HTTP reachability does not prove business
correctness or downstream selection.

Logs default to the application. Select an approved supporting container on a
composite override, including completed/failed init work:

```sh
envy composition logs COMPOSITION_ID --component pricing --container bootstrap
envy composition logs COMPOSITION_ID --component pricing \
  --container database-proxy --previous --tail-lines 100
```

REST accepts `container` on component logs; MCP exposes it on
`get_component_logs`. The dashboard offers a container field and previous-instance
option. Selection cannot access arbitrary Pods/namespaces or mesh containers.
Supporting selection is limited to captured override policies; inherited logs
remain shared-baseline. Existing byte, Pod and time limits apply. Application log
contents can contain sensitive data emitted by the application; Envy does not
redact arbitrary application output or retain it after workload deletion.

## Acceptance and boundaries

`make test-composite-e2e` runs a disposable cluster with a generic application
behind a regular proxy, one-shot init work, a native sidecar, synthetic
configuration/Secrets and a synthetic identity annotation. It needs no private
application repository or cloud credentials. See the script and tests for exact
assertions and retained diagnostic paths.

Validation on 21 September 2026: the full `make check` suite and synthetic
composite lifecycle acceptance passed in a disposable Kubernetes cluster. The
acceptance exercised two previews, app-only image updates, proxy traffic, copied
configuration, native-sidecar failure reporting, selected logs and cleanup.
An inherited composite also returns its policy-selected application logs.
The existing `make test-derived-e2e` acceptance passed against an Argo-managed
baseline, including update/recovery and TTL cleanup. Both disposable clusters
were deleted after validation.

Provider tests also cover rejection, approval changes, dependency rewriting,
native-sidecar quota, identity conflicts/repair and bounded init logs. Run the
existing derived-preview acceptance for backward compatibility. Local acceptance
does not prove cloud IAM, database safety or messaging instrumentation.

Generic workers, Jobs, schedules, automatic migrations, arbitrary Pod shapes and
broker integrations beyond the existing Pub/Sub contract remain outside this
profile. Real applications still need their own side-effect and business tests.
