import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { apiClient } from "../../lib/api-client";
import { PRPreviewOwnership } from "./PRPreviewOwnership";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

it("shows current requested and deployed revisions on the owned composition", async () => {
  vi.spyOn(apiClient, "prPreview").mockResolvedValue({
    id: "preview",
    policy: {
      project: "demo",
      repository: "backend",
      enabled: true,
      baseline: "staging",
      components: ["api"],
      ttl: "1h",
      workflow_id: 11,
    },
    number: 7,
    pr_url: "https://github.com/acme/backend/pull/7",
    status: "waiting_build",
    reason: "Waiting for report",
    requested_sha: "new-head",
    deployed_sha: "old-head",
    composition_id: "composition",
    generation: 2,
    terminal: false,
  });
  render(<PRPreviewOwnership id="preview" compositionId="composition" />);
  expect(
    await screen.findByRole("link", { name: "Pull request #7" }),
  ).toBeTruthy();
  expect(screen.getByText(/Requested: new-head/)).toBeTruthy();
  expect(screen.getByText(/Deployed: old-head/)).toBeTruthy();
  expect(screen.getByText(/Waiting for report/)).toBeTruthy();
});
