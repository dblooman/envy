# Envy documentation site

This directory contains Envy's Astro 7 and Starlight documentation site. The
site documents the provider-independent control plane, deployed reference
baselines, compositions, delivery interfaces, and agent integrations.

## Local development

From the repository root:

```bash
cd site
pnpm install
pnpm dev
```

Open the local URL printed by Astro. The landing page and documentation use
the same site build, so changes to `src/components/` and
`src/content/docs/` can be reviewed together.

## Useful commands

| Command | Purpose |
| :--- | :--- |
| `pnpm dev` | Start the local documentation server. |
| `pnpm build` | Generate the static site in `dist/`. |
| `pnpm preview` | Serve the generated site locally. |
| `pnpm dlx @astrojs/check` | Run Astro's type and content checks without adding a local dependency. |

## Project layout

```text
site/
├── astro.config.mjs       # Starlight title, navigation, and integrations
├── src/components/        # Landing-page sections and interactions
├── src/content/docs/      # Versioned documentation pages
├── src/styles/custom.css  # Shared theme and accessibility refinements
└── public/                # Static site assets
```

Keep examples aligned with the repository's authoritative contracts:

- `examples/shop/application.json` for catalog manifests
- `api/openapi.yaml` for REST request and response shapes
- `internal/cli/` for delivery CLI flags and limits
- `internal/mcp/` for MCP tool inputs and limits

The local demo uses a catalog baseline ID named `staging`. In documentation
prose, describe the concept as a deployed/reference baseline because the
baseline may instead represent `main`, a release, or another approved
environment.
