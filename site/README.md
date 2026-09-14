# Envy documentation site

This directory contains Envy's Astro 7 and Starlight documentation site. The
site documents the provider-independent control plane, deployed reference
baselines, compositions, delivery interfaces, and agent integrations.

## Local development

From the repository root:

```bash
cd site
pnpm install --frozen-lockfile
pnpm dev
```

Use Node 24.8+ (Node 24) and pnpm 10.20.0. Open `http://localhost:4321/envy/`. The landing page and documentation use
the same site build, so changes to `src/components/` and
`src/content/docs/` can be reviewed together.

## Useful commands

| Command          | Purpose                                        |
| :--------------- | :--------------------------------------------- |
| `pnpm dev`       | Start the local documentation server.          |
| `pnpm build`     | Generate the static site in `dist/`.           |
| `pnpm preview`   | Serve the generated site locally.              |
| `pnpm typecheck` | Run the pinned Astro type and content checker. |

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

## Diagrams

Service and workflow diagrams share `src/components/ServiceDiagram.astro`.
Use it from an MDX page instead of adding ASCII art or one-off diagram markup:

```mdx
import ServiceDiagram from "../../../components/ServiceDiagram.astro";

<ServiceDiagram name="composition" />
```

Define nodes and labeled connections in `src/diagrams/graphs.ts`. Reuse the
`composition` graph wherever the same baseline/override request path is shown.
Blue identifies shared baseline services, green preview overrides, purple agent
or external actions, and slate control-plane or routing steps. Nodes also carry
text labels, so color is never the only distinction. Dashed connectors show retry
paths.

ELK (`elkjs`) computes orthogonal connectors during the Astro build; the browser
receives SVG and CSS, with no diagram library runtime. Graphs keep their text size
and scroll inside their own panel when space is limited. Each includes a caption
and an expandable text version of its connections. The optional `wide` prop places
the caption beside the graph on the homepage. Give repeated instances on the same
page distinct `id` props so their SVG title and arrow IDs remain unique.

Run `pnpm check:diagrams` to verify that all graphs render complete connections
without crossing service cards. Run `pnpm build` after MDX or graph changes and
check both themes at desktop and mobile widths.

## Validation and GitHub Pages

`pnpm check` runs lint, formatting checks, type checks, unit/diagram tests, the
production build, and a local crawl of links, anchors, assets, and sitemaps.
`pnpm format` formats handwritten site files. Scrollable named regions intentionally
remain keyboard-focusable. Review both themes and mobile widths after visual edits.

The site is hosted at https://dblooman.github.io/envy/. The origin and base path
live in `src/lib/paths.mjs`. Astro components use `withBase()` for internal links;
Markdown/MDX links can start at `/` because the shared Remark transform adds the
prefix. External URLs and fragment-only links are unchanged. Do not hardcode the
`/envy/` prefix into content. `pnpm preview` serves the generated production build
at `/envy/`, including the Pagefind search index.

The **CI and Pages** workflow checks pull requests without deployment credentials.
On a successful push to `main` (or manual run on `main`), it deploys `site/dist`
through the GitHub Pages artifact API after every fast check succeeds.

Maintainer setup:

1. In repository **Settings → Pages**, choose **GitHub Actions** as the source.
2. Keep the custom-domain field empty for the project URL. No CNAME is required.
3. Allow deployments from `main` in the `github-pages` environment.
4. Run **CI and Pages** on `main` after merging the configuration.
5. Follow the deployment URL and verify the homepage, a direct documentation URL,
   search results, favicon, mobile navigation, and an unknown URL's 404 recovery.

Forks run checks but do not deploy by default. To publish a fork, update the shared
origin/base and the deployment job's repository guard together. This workflow
publishes documentation only; `web/` is the Envy dashboard served by the API.
