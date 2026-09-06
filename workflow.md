Yes. We should provide an onboarding kit, an agent workflow, and a frontend binding integration. Otherwise each agent will have to rediscover how to connect everything.
These are proposed additions; the current implementation provides only part of this.

1. Setup: register the project and prove it works once
   Envy needs a repeatable setup process that captures:
   - Components, their repositories, image registries, and approved deployment configuration.
   - Existing staging service addresses and dependencies.
   - Which component receives public traffic—GraphQL in your example.
   - Preview DNS/TLS, Kubernetes access, and which resources Envy owns versus Argo.
   - Credentials for agents and CI.
     I would add an initialization command and a diagnostic command. The diagnostic should verify image pulls, workload creation, ingress routing, and request-context propagation through GraphQL and the backend.
     Setup should produce a real working preview. Engineers then reuse that registration for subsequent changes.
2. Workflow: a reusable skill plus small repository instructions
   We should ship both, with different responsibilities:
   Artifact What belongs there
   Envy skill Discover → resolve images → create/update → wait → diagnose → verify → report
   Each repository’s AGENTS.md Envy project/component identity, build instructions, test commands, and reference to the skill
   Machine-readable project configuration Repository mappings, baseline bindings, deployment profiles, and frontend configuration contract
   API state Composition identity, participating revisions/PRs, current generation, status, and expiry

   The skill should explain how to reuse an existing composition, handle conflicting updates, and distinguish deployment readiness from passing application tests.
   We also need MCP discovery and composition lookup tools. Currently an agent can operate a composition, but finding the right project and existing preview requires more work than it should.
   Multiple agents should share an explicit composition ID. For the first version, one coordinator can submit the combined changes. Shared state should live in Envy, so the workflow survives an agent restarting.

3. Frontend: one supported way to receive its API URL
   For the application frontend, the minimum requirement is straightforward: its GraphQL client must take a configurable base URL.
   I would support build-time configuration first:
   Resolve composition → obtain GraphQL URL → configure frontend build
   Keeping the composition URL stable means later backend and GraphQL updates do not require rebuilding the frontend.
   We also need to verify the actual browser path: HTTPS, CORS, authentication/cookies, and login callbacks where applicable. A successful server-side API probe does not prove the browser can use it.
   If a requested composition is missing or expired, the integration should report that clearly. Silently connecting the preview to staging could make an engineer test the wrong backend.
   If you also mean Envy’s own frontend, I would add a composition details view showing participating PRs, exact versions, inherited services, the external frontend link, and separate infrastructure and application-check results.
4. Cloudflare Pages: a build adapter and a durable binding
   We should supply a small script that runs before the normal Pages build. Cloudflare provides the branch, commit SHA, and deployment URL as build variables. Cloudflare build configuration.
   The proposed flow is:
   Agent creates composition and associates frontend revision
   ↓
   Pages build resolves that association
   ↓
   Script supplies GraphQL URL to the frontend build
   ↓
   Integration records the Pages deployment URL against the composition
   ↓
   Verification updates the PR
   That association is an important missing feature: frontend, GraphQL, and backend branches can have different names.
   We must also handle Pages starting before the association exists. The script should wait with a deadline and provide a clear retry path. If we want strict sequencing instead, CI can create the composition, build the frontend, and deploy through Cloudflare’s supported Direct Upload workflow. Cloudflare CI deployment.
   The build receives the public API URL; Envy credentials stay in the build or integration environment.
   I would implement multiple overrides and a configurable entrypoint first, then project discovery/setup, the skill and repository instructions, and one Cloudflare example that proves the complete browser-to-staging chain. PR comments and webhook triggers can then reuse that verified workflow.
