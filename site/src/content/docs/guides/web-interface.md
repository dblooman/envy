---
title: Using the Web Interface
description: Find, create, open, and troubleshoot previews with a screenshot walkthrough of the Envy dashboard.
---

The web interface calls environments **previews**. The API, CLI, and URLs still use **composition** for the same resource.
Preparation drafts, server plans, and structured diagnosis were merged after
v0.8.0 and require a newer source build until a release includes them.

This walkthrough uses the Slate and teal theme with **Demo simulation** enabled. Screenshots illustrate the core flow; message-isolation controls require the operator setup described below. Names, URLs, readiness, and expiry times in the screenshots are sample data; no cluster resources were created. Select a screenshot to open it at full size.

## Open the dashboard

The released Envy image includes the dashboard. After [installing with Helm](/getting-started/installation/), use your team's Envy URL or the guide's port-forward at `http://localhost:8081`. You do not need to install the CLI or run a separate frontend server.

To look around with sample data, select **Explore demo** on the login screen. Once inside the workspace, the simulation switch is under **Installation → Demo**. You can also follow the screenshots below without installing anything.

Live installations use their configured login mode: local dev opens as Admin, password mode shows a username/password form, and Google mode shows **Continue with Google**. Browser tokens and server URLs are no longer entered manually. The signed-in identity and **Sign out** / **Sign out everywhere** controls appear under **Installation**.

To configure login for your installation, follow [Authentication & Google Sign-in](/guides/authentication/).

Turn off Demo simulation when you want to work with the live API. The simulator does not deploy workloads or provide working preview endpoints; live diagnostics and revision history require a connected installation. Frontend contributors can [run the simulation from source](/development/dashboard/) without a cluster.

## Onboard your application

In Live Mode, choose **Catalog & baselines → Onboard application**. Describe the
existing baseline, choose deployment-derived or manual HTTP profiles, validate,
and explicitly register. Then discover and approve any deployment-derived
services before opening the preview creation flow. See the
[guided onboarding walkthrough](/getting-started/onboarding/#guided-dashboard-onboarding)
for fields, permissions, and verification semantics.

Registration saves the baseline without creating workloads. Resume saved setup
from **Prepare overrides** on its Catalog card. During the first two onboarding
steps, **Save preparation** stores non-secret fields for **Load saved
preparation** after a reload; environment values must be re-entered. Unsaved
edits are discarded when you leave onboarding. The JSON editor remains under
**Advanced registration**.

## Find and open a preview

Select **Previews** in the workspace navigation.

[![Preview list showing ready and provisioning cards, changed services, remaining lifetime, search, filters, and Open preview actions.](/images/web-interface/previews.png)](/images/web-interface/previews.png)

1. Search by preview name, component, or image. Use **Ready**, **In progress**, or **Needs attention** to narrow the list; **All** also includes terminated previews.
2. Read the changed services, readiness message, and remaining lifetime on each card. Use the layout buttons to switch between cards and rows.
3. Select a **preview name** to inspect it. Select **Open preview** to visit its endpoint, or copy the URL to share it with a teammate who has access.

**Endpoint ready · routing unverified** means endpoint readiness has been reported, but request routing and context propagation have not been verified. Do not treat endpoint reachability as proof that every request reached the intended overridden services.

## Create a preview

Select **New preview** or **Create preview** in the navigation. The configuration summary follows your choices through all three steps.

### 1. Choose baseline

Enter a recognizable name such as `pricing-review`, then choose your project and baseline. The baseline provides the services you leave unchanged; `staging` is the demo's baseline name, not a required naming convention.

[![Choose baseline step with pricing-review as the name, Demo Microservices as the project, staging as the baseline, and the configuration summary.](/images/web-interface/create-baseline.png)](/images/web-interface/create-baseline.png)

Select **Isolate Pub/Sub messages** only after completing [Google Pub/Sub setup](/guides/pubsub-isolation/). The choice is fixed for this preview and does not require a consumer override. Expand **Advanced configuration** if you need to adjust the preview lifetime or other optional settings. Otherwise keep the installation defaults and select **Continue**.

### 2. Select changes

Select the components to override and choose their versions. In this example, `service-b` uses `envy/service-b:v2`; `gateway` and `service-a` stay on the shared baseline.

[![Select changes step with service-b selected and its direct image set to envy/service-b:v2; gateway and service-a are unselected.](/images/web-interface/create-changes.png)](/images/web-interface/create-changes.png)

You can select up to three components, or select none to inherit the complete baseline. In a configured live installation, you can choose published Git builds when available. Direct images do not provide source provenance. Select **Continue** after reviewing your choices.

### 3. Review and create

Check the name, baseline, service versions, and lifetime in the summary. The
server plan shows selected and inherited workloads, approved revisions,
destination, resource estimate, and any blocker with a next action. Refresh it
after changing a selection. Planning reserves no capacity; Envy checks the
contract again at creation. Use **Back** to correct a choice, then select
**Create preview** to submit it.

[![Review and create step summarizing the service-b override, shared baseline behavior, and final Create preview button.](/images/web-interface/create-review.png)](/images/web-interface/create-review.png)

In Live Mode, submission creates a composition and starts provisioning. Follow its status before opening the endpoint. Your unfinished draft remains in the current tab while you navigate, but reloading clears it.

## Inspect a preview

Select a preview name from the list to open its workspace.

[![Preview Overview with the endpoint, Open preview button, generation, remaining lifetime, and an explicit routing-unverified message.](/images/web-interface/preview-detail.png)](/images/web-interface/preview-detail.png)

The detail page separates four tasks:

| Section      | Use it to                                                                                                                                                                                                        |
| ------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Overview** | Open or copy the endpoint, check expiry and generation, inspect shared and overridden services, and read the current diagnosis and verification evidence. Frontend bindings are separate from backend readiness. |
| **Changes**  | Inspect source revisions, image artifacts, and desired-state changes.                                                                                                                                            |
| **Logs**     | Inspect bounded application logs and supported live diagnostics. Inherited logs may include traffic from other previews.                                                                                         |
| **History**  | Review actor requests and durable lifecycle events.                                                                                                                                                              |

[![Changes section showing service-b's image, unavailable source provenance for a direct image, and revision history requiring Live Mode.](/images/web-interface/preview-changes.png)](/images/web-interface/preview-changes.png)

_The demo labels unavailable live evidence explicitly. An image reference alone does not establish the source commit or verified routing._

For a preview that is failing or taking too long, start with the diagnosis on
**Overview**, then correlate its findings with **Logs**, **History**, and the
selected images in **Changes**. Verification evidence can be current, stale,
failed, unavailable, or of unknown coverage; a passed HTTP check establishes
reachability, not every downstream route. See
[Diagnostics & Error Codes](/reference/diagnostics/) for troubleshooting and
[Routing & Ingress Contract](/reference/routing/) for verification semantics.

## Update, clean up, and configure

- **Update preview** changes an existing preview's selected revisions and override membership, including returning all services to baseline inheritance. It keeps the URL and original expiry. After submitting an update, follow the new generation and readiness before testing again.
- **Destroy** removes the preview when testing is complete. Check the preview name in the confirmation before proceeding; the shared baseline remains available.
- **Catalog & baselines** groups catalog information, sources, and registration under platform administration.
- **Installation** separates read-only server configuration, browser appearance preferences, and local development connections.

Everyday preview work stays under **Workspace** in the navigation; platform configuration stays under **Platform**.

For label-driven PR environments, use **Catalog → Sources** to discover installed
GitHub repositories and **Catalog → GitHub** to configure policies, inspect webhook
health, and stop or restart previews. See [GitHub App & PR Previews](/integrations/github-app/)
for permissions and setup. App-owned previews update automatically; generic updates
are blocked, and destroying one also stops its automation.
