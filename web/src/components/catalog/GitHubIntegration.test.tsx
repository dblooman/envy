import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { GitHubIntegration } from "./GitHubIntegration";
import { SourceRepositories } from "./SourceRepositories";
import { apiClient } from "../../lib/api-client";
import type { PRPreview, PreviewPolicy } from "../../types/github";

const context = vi.hoisted(() => ({
  isDemoMode: false,
  projects: [{ id: "shop", name: "Shop" }],
  baselines: [{ id: "staging", project: "shop" }],
  components: [{ id: "pricing", project: "shop", overridable: true }],
}));
vi.mock("../../context/ApiContext", () => ({ useEnvyApi: () => context }));
const repository = {
  project: "shop",
  id: "backend",
  github_repository: "acme/shop",
  installation_id: 42,
  enabled: true,
  images: { pricing: "registry.example.com/pricing" },
};
const policy: PreviewPolicy = {
  project: "shop",
  repository: "backend",
  enabled: true,
  baseline: "staging",
  components: ["pricing"],
  workflow_id: 11,
  ttl: "8h",
};
const preview: PRPreview = {
  id: "preview",
  policy,
  number: 7,
  pr_url: "https://github.com/acme/shop/pull/7",
  status: "waiting_build",
  reason: "Waiting for the latest build",
  requested_sha: "new-revision",
  deployed_sha: "old-revision",
  composition_id: "composition",
  generation: 1,
  url: "https://preview.example.com",
  expires_at: "2026-09-16T20:00:00Z",
  terminal: false,
};
beforeEach(() => {
  vi.spyOn(apiClient, "githubStatus").mockResolvedValue({
    configured: true,
    webhook_configured: true,
  });
  vi.spyOn(apiClient, "previewPolicies").mockResolvedValue({ items: [policy] });
  vi.spyOn(apiClient, "listSourceRepositories").mockResolvedValue([repository]);
  vi.spyOn(apiClient, "prPreviews").mockResolvedValue({ items: [preview] });
  vi.spyOn(apiClient, "controlPRPreview").mockResolvedValue({
    ...preview,
    status: "stopped",
    terminal: true,
  });
  vi.spyOn(apiClient, "savePreviewPolicy").mockResolvedValue(policy);
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
it("distinguishes pending and deployed revisions and controls the preview by ID", async () => {
  const user = userEvent.setup();
  render(<GitHubIntegration />);
  await screen.findByText(/Waiting for the latest build/);
  expect(screen.getByText(/Requested: new-revision/).textContent).toContain(
    "Deployed: old-revision",
  );
  await user.click(screen.getByRole("button", { name: "Stop" }));
  expect(apiClient.controlPRPreview).toHaveBeenCalledWith("preview", "stop");
  await user.click(screen.getByRole("button", { name: "Restart" }));
  expect(apiClient.controlPRPreview).toHaveBeenCalledWith("preview", "restart");
});
it("edits the explicit component policy without changing unrelated previews", async () => {
  const user = userEvent.setup();
  render(<GitHubIntegration />);
  await screen.findByRole("option", { name: /acme\/shop/ });
  await user.selectOptions(
    screen.getByLabelText("Source repository"),
    "shop/backend",
  );
  await user.clear(screen.getByLabelText("Lifetime"));
  await user.type(screen.getByLabelText("Lifetime"), "2h");
  await user.click(
    screen.getByRole("button", { name: "Validate permissions and save" }),
  );
  await waitFor(() =>
    expect(apiClient.savePreviewPolicy).toHaveBeenCalledWith({
      ...policy,
      ttl: "2h",
    }),
  );
  expect(apiClient.controlPRPreview).not.toHaveBeenCalled();
});
it("discovers repository installation IDs and permissions", async () => {
  vi.spyOn(apiClient, "githubInstallations").mockResolvedValue({
    items: [
      { id: 42, account: { login: "acme" }, permissions: { contents: "read" } },
    ],
    page: 1,
    has_more: false,
  });
  vi.spyOn(apiClient, "githubRepositories").mockResolvedValue({
    items: [{ id: 1, full_name: "acme/shop" }],
    page: 1,
    has_more: false,
  });
  const user = userEvent.setup();
  render(<SourceRepositories />);
  await screen.findByText(/acme\/shop · backend/);
  await user.click(screen.getByText("Register repository"));
  await user.click(
    screen.getByRole("button", { name: "Discover installed repositories" }),
  );
  await screen.findByRole("option", { name: /acme\/shop — contents: read/ });
  await user.selectOptions(screen.getByLabelText("Accessible repository"), "0");
  expect(
    (screen.getByLabelText("GitHub installation ID") as HTMLInputElement).value,
  ).toBe("42");
  expect(
    (screen.getByLabelText("GitHub repository") as HTMLInputElement).value,
  ).toBe("acme/shop");
});
