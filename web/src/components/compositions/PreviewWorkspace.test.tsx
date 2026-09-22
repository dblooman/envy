import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { CompositionDetailView } from "./CompositionDetailView";
import { apiClient } from "../../lib/api-client";
import {
  INITIAL_MOCK_COMPOSITIONS,
  MOCK_BASELINES,
  MOCK_COMPONENTS,
} from "../../lib/mock-data";
import type { FrontendBindingView } from "../../types/api";
vi.mock("../../context/ApiContext", () => ({
  useEnvyApi: () => ({
    isDemoMode: false,
    baselines: MOCK_BASELINES,
    components: MOCK_COMPONENTS,
    lastRefreshed: "2026-09-22T12:00:00Z",
  }),
}));
const composition = INITIAL_MOCK_COMPOSITIONS[0];
const binding = (name: string): FrontendBindingView => ({
  binding: {
    project: composition.project,
    frontend: name,
    revision: "a".repeat(40),
    composition: composition.id,
    repository: "https://example.test/repo",
    version: 1,
    url: `https://${name}.example.test`,
    created_at: "2026-09-22T10:00:00Z",
    updated_at: "2026-09-22T10:00:00Z",
    check: {
      composition_generation: 1,
      status: "passed",
      message: "Reported check",
      reported_at: "2026-09-22T10:00:00Z",
    },
  },
  composition_phase: "ready",
  composition_generation: 2,
  expires_at: "2099-01-01T00:00:00Z",
  ready: true,
  verification_level: "reachability",
  check_state: "stale",
});
beforeEach(() => {
  vi.spyOn(apiClient, "verification").mockResolvedValue({ items: [] });
  vi.spyOn(apiClient, "observability").mockResolvedValue({ items: [] });
  vi.spyOn(apiClient, "listFrontendBindings").mockResolvedValue({ items: [] });
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
it("chooses between applications and labels reported stale checks", async () => {
  vi.mocked(apiClient.listFrontendBindings).mockResolvedValue({
    items: [binding("web"), binding("admin")],
  });
  const open = vi.spyOn(window, "open").mockImplementation(() => null);
  const user = userEvent.setup();
  render(
    <CompositionDetailView
      composition={composition}
      section="Overview"
      onSectionChange={vi.fn()}
      onBack={vi.fn()}
      onUpdate={vi.fn()}
      onDestroy={vi.fn()}
    />,
  );
  await screen.findByRole("button", { name: "Open application" });
  await user.selectOptions(
    screen.getByLabelText("Application"),
    `admin/${"a".repeat(40)}`,
  );
  await user.click(screen.getByRole("button", { name: "Open application" }));
  expect(open).toHaveBeenCalledWith(
    "https://admin.example.test",
    "_blank",
    "noopener,noreferrer",
  );
  expect(screen.getByText(/passed · stale · tested revision/)).toBeTruthy();
});
it("keeps PR-managed update restrictions behind Manage", async () => {
  const user = userEvent.setup();
  vi.spyOn(apiClient, "prPreview").mockRejectedValue(new Error("Unavailable"));
  render(
    <CompositionDetailView
      composition={{ ...composition, pr_preview_id: "pr-test" }}
      section="Overview"
      onSectionChange={vi.fn()}
      onBack={vi.fn()}
      onUpdate={vi.fn()}
      onDestroy={vi.fn()}
    />,
  );
  expect(screen.queryByRole("button", { name: "Update preview" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Manage" }));
  expect(
    (
      screen.getByRole("button", {
        name: "Update preview",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
});
it("does not present removed workloads as currently ready", async () => {
  render(
    <CompositionDetailView
      composition={{ ...composition, phase: "destroyed" }}
      section="Overview"
      onSectionChange={vi.fn()}
      onBack={vi.fn()}
      onUpdate={vi.fn()}
      onDestroy={vi.fn()}
    />,
  );
  expect(
    screen.getByRole("heading", { name: "Last recorded services" }),
  ).toBeTruthy();
  expect(screen.getAllByText("Historical observation").length).toBe(
    Object.keys(composition.components).length,
  );
  expect(
    (screen.getByRole("button", { name: "Open API" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
});
