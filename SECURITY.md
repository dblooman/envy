# Security policy

Envy is pre-1.0. Security fixes target the current `main` branch; older commits do
not have a separate maintenance commitment.

Report vulnerabilities through [GitHub private vulnerability reporting](https://github.com/dblooman/envy/security/advisories/new).
Include the affected commit, installation/profile, reproduction steps, and impact.
Keep credentials, customer data, and exploit details out of public issues. If the
private reporting form is unavailable, open a public issue requesting a private
contact without disclosing the vulnerability.

Local development uses loopback endpoints and generated credentials under `.envy/`.
Do not expose the local quickstart to untrusted networks. For a shared installation,
follow [authentication](docs/authentication-and-activity.md),
[installation](docs/installation.md), and [operations](docs/operations.md).
Preview workloads share baseline services and data; consult the documented sharing
semantics before running destructive tests. Envy is not a hostile-code sandbox.

Maintainers: enable private vulnerability reporting and GitHub secret scanning
when making the repository public. See the release checklist in
[the readiness audit](docs/open-source-readiness.md).
