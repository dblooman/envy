// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import { unified } from "@astrojs/markdown-remark";
import {
  siteOrigin,
  siteBase,
  withBase,
  remarkBaseLinks,
} from "./src/lib/paths.mjs";

// https://astro.build/config
export default defineConfig({
  site: siteOrigin,
  base: siteBase,
  trailingSlash: "always",
  markdown: { processor: unified({ remarkPlugins: [remarkBaseLinks] }) },
  integrations: [
    starlight({
      title: "Envy",
      description:
        "Ephemeral microservice preview environments for Kubernetes with Istio, Cilium, or Linkerd with zero-copy baseline sharing.",
      head: [
        {
          tag: "link",
          attrs: {
            rel: "icon",
            href: withBase("/favicon.svg"),
            type: "image/svg+xml",
          },
        },
      ],
      logo: {
        src: "./src/assets/logo.svg",
      },
      customCss: ["./src/styles/custom.css"],
      components: { PageTitle: "./src/components/PageTitle.astro" },
      disable404Route: true,
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/dblooman/envy",
        },
      ],
      sidebar: [
        {
          label: "Overview",
          items: [
            { label: "What is Envy?", slug: "overview/introduction" },
            {
              label: "Architecture & Mechanics",
              slug: "overview/architecture",
            },
            {
              label: "Sharing & Isolation",
              slug: "overview/sharing-semantics",
            },
          ],
        },
        {
          label: "Getting Started",
          items: [
            {
              label: "Choose Your Mesh",
              slug: "getting-started/mesh-installation",
            },
            { label: "Local Quickstart", slug: "getting-started/quickstart" },
            { label: "Web Interface", slug: "guides/web-interface" },
            {
              label: "Authentication & Google Sign-in",
              slug: "guides/authentication",
            },
            {
              label: "Onboard an Application",
              slug: "getting-started/onboarding",
            },
            { label: "Multi-Service Overrides", slug: "guides/example" },
          ],
        },
        {
          label: "Agent Skills & MCP",
          items: [
            {
              label: "Model Context Protocol (MCP)",
              slug: "agents/mcp-server",
            },
            { label: "Agent Workflow Kit", slug: "agents/workflow" },
          ],
        },
        {
          label: "API & CLI Reference",
          items: [
            { label: "Delivery CLI Reference", slug: "reference/cli" },
            { label: "REST API Reference", slug: "reference/api" },
            { label: "Routing & Ingress Contract", slug: "reference/routing" },
            {
              label: "Diagnostics & Error Codes",
              slug: "reference/diagnostics",
            },
          ],
        },
        {
          label: "Integrations",
          items: [
            {
              label: "Argo CD & Deployment Previews",
              slug: "integrations/argo-cd",
            },
            { label: "GitHub Actions", slug: "integrations/github-actions" },
            {
              label: "Cloudflare Pages & Frontends",
              slug: "integrations/cloudflare-pages",
            },
          ],
        },
      ],
    }),
  ],
});
