# Agent Instructions for Envy Preview Environments

This repository participates in the **Envy Preview Platform**.

## Repository Identity
See `.envy/project.json` in this repository for machine-readable identity:
- **Project**: Defined in `.envy/project.json` (`project`)
- **Component**: Defined in `.envy/project.json` (`component`)
- **Baseline**: `staging`
- **Public Entrypoint**: Defined in `.envy/project.json` (`entrypoint`)

## Creating or Updating Previews
When you need to spin up or update a preview environment for this repository:
1. Use the **Envy skill** (`skills/envy/SKILL.md`) or the `delivery` CLI / Envy MCP server.
2. Read `.envy/project.json` to get the project ID and component name.
3. Call `lookup_composition` (or `delivery composition lookup`) using your commit SHA or branch name.
4. If a composition exists:
   - Call `update_composition` with the updated image and commit SHA.
5. If no composition exists:
   - Build/verify your image tag.
   - Call `create_composition` with the component override and revision metadata.
6. Always call `wait_for_composition` and verify `RequestRoutingVerified` before sharing preview links.

## Frontend Builds
If building a frontend application that connects to this backend:
- Run the build using `delivery frontend run -- <build command>` (or `./adapters/cloudflare-pages/build.sh <build command>`).
- This will inject `VITE_GRAPHQL_URL`, `NEXT_PUBLIC_GRAPHQL_URL`, and `API_URL` pointing to the preview composition.
- Do NOT hardcode staging URLs into preview builds.
