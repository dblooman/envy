# Onboarding readiness workflow

This reusable helper collects a point-in-time readiness report for an existing
application. It works with `http-small`, `deployment` and `deployment-composite`.
It never registers a catalog, approves a profile, creates a preview, changes IAM,
or runs an application command. The server may record discovery audit events and catalog validation probes its
configured ingress path; choose a safe read endpoint. Use it before the first real preview and rerun it
when source configuration, policy or external prerequisites change.

## Prepare the inputs

1. Prepare the application's complete `envy/v1` catalog using
   [application onboarding](../../docs/onboarding.md). The application/platform
   owns the baseline, mesh and ingress. For composite workloads, first install
   the [scoped policy](../../docs/composite-previews.md).
2. Copy `readiness.example.json` beside your own catalog and update its relative
   `catalog` path. The checked-in example refers to the shop demonstration;
   it contains no evidence that a real installation is ready.
3. Select one to three overridable component IDs. For derived profiles, the helper
   reuses the saved approval's selection and configuration replacements. When
   there is no approval it discovers with an empty selection, allowing the
   operator policy/default discovery rules to select the source.
4. Assign each prerequisite an owner and record evidence. Never place passwords,
   tokens, Secret payloads or credential-bearing URLs in either file.

| Prerequisite | Evidence needed for the selected pilot |
| --- | --- |
| `application_effects` | Migrations, consumers, publishers, seeders and timers are disabled or deliberately isolated; startup/retry paths tested. |
| `identity` | Approved identity works in generated namespaces; external access handoff and cleanup are defined. |
| `dependencies` | DNS, networking, TLS and permissions work for shared non-production dependencies; shared effects disclosed. |
| `schema` | Compatible schema and synthetic data prepared externally; no implicit preview migration. |
| `image` | Candidate immutable digest and source revision are available; registry pulls verified in the target environment. |
| `business_scenario` | A named synthetic read scenario, expected output and out-of-process smoke command are prepared. |

Statuses are `pending`, `confirmed`, or `not-applicable`. Every gate needs an owner;
confirmed gates need an evidence reference, and not-applicable gates need an
explanation. These are operator declarations: the helper does not execute smoke
commands or verify evidence links. Evidence should identify the environment,
source/image revision and date so reviewers can assess whether it is still valid.
`business_scenario` describes a prepared test; after creation, the test still has
to pass against baseline and preview URLs.

## Run

Python 3 with its standard library is sufficient:

```sh
python3 integrations/onboarding/readiness.py \
  --config integrations/onboarding/readiness.example.json --check-config

python3 integrations/onboarding/readiness.py \
  --config /path/to/application/readiness.json \
  --api-url https://envy.example.test \
  --token-file /path/to/ignored/token \
  --output /path/to/ignored/readiness-report.json
```

`ENVY_API_URL` and `ENVY_API_TOKEN_FILE` provide defaults. HTTPS is required except
for loopback HTTP development APIs. Redirects are refused, including same-origin
redirects, to keep bearer credentials at the configured endpoint. Each request has
a 30-second timeout and a 2 MiB response limit. HTTP errors record their status
without copying response bodies into the report. Reports written to disk use
atomic replacement and owner-only permissions. Without `--output`, JSON goes to
stdout; treat redirected output as operator evidence with appropriate access.

Exit status is `0` for `ready_for_creation_review`, `2` for unresolved readiness,
and `1` for malformed input or transport/protocol failures that prevent reporting.
`--check-config` is local input validation only and makes no readiness claim.

## What the report checks

The helper calls `POST /v1/catalog/validate`, locates the registered baseline with
pagination, and compares persisted baseline/component records with the normalized
catalog. Validation alone can succeed for unregistered entries. If registration
is missing, explicitly run `envy catalog apply --file application.json`, then rerun.
Conflicting immutable records require correcting the input or a new catalog ID.

For each selected derived component it reads the saved approval and calls
`preview-profile/discover` with that approval's saved selection. Zero blockers,
the same source UID and the same contract fingerprint establish approval currency,
matching preview creation semantics. A changed inspection hash alone does not
invalidate approval. Current approval revisions are included for the eventual
`--expected-preview-revision` guards. Manual profiles need no derived approval.

Missing/stale approvals, source-read restrictions and discovery blockers remain
visible as pending work. Use the dashboard or existing CLI to discover, review
replacements and approve explicitly. This helper never manufactures approval or
sets `confirm_connectivity`. Discovery configuration and Secret values are not
copied into the readiness report; source identity, blockers, warnings and named
RBAC requirements are retained.

`ready_for_creation_review` means the reported API checks passed and all operator
gates have a declaration. It is not workload readiness, live cloud proof,
side-effect isolation, an authorization token or a guarantee the next create
succeeds. Creation revalidates source and policy; quota, image resolution/pulls,
namespace-specific IAM and actual business behavior still need live validation.
No prerequisite hook prevents somebody bypassing this helper and creating through
Envy directly.

After review, create two previews with distinct digests, run the application-owned
smoke scenario against baseline and both previews, update one image, inspect
failure diagnostics, then destroy/expire both and verify cleanup. Preserve evidence
without credentials. The synthetic composite and derived acceptance suites remain
Envy's generic regression gates.

## First application rollout

Follow the [application rollout plan](../../docs/application-rollout-plan.md),
starting with the rendered deployment contract. The generic foundation is
implemented; this workflow does not fill in unknown namespaces, rendered container
names, cloud grants or business fixtures. Keep all six prerequisites pending until
their owners supply evidence. Application preview-mode controls and the
namespace-specific identity handoff must be resolved before the first real preview.

Keep application-specific manifests, identities and evidence in the deploying
organisation's own configuration repository. Public examples and acceptance tests
should use synthetic services and data.

Run helper tests with:

```sh
python3 -m unittest discover -s integrations/onboarding -p 'test_*.py'
```
