import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { CompositionHistory } from "./CompositionHistory";
import { apiClient } from "../../lib/api-client";
import { INITIAL_MOCK_COMPOSITIONS } from "../../lib/mock-data";
import type { LifecycleEvent } from "../../types/api";
vi.mock("../../context/ApiContext", () => ({
  useEnvyApi: () => ({ isDemoMode: false }),
}));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
it("collapses routine startup observations while keeping failures and recovery visible", async () => {
  const composition = INITIAL_MOCK_COMPOSITIONS[0];
  const event = (id: string, message: string): LifecycleEvent => ({
    id,
    composition: composition.id,
    project: composition.project,
    generation: 1,
    type: "observation_changed",
    phase: "provisioning",
    occurred_at: `2026-09-22T10:00:0${id}Z`,
    operation: composition.latest_operation,
    conditions: [{ type: "WorkloadsReady", status: false, message }],
  });
  const failure = event("3", "");
  failure.conditions.push({
    type: "RouteVerified",
    status: false,
    message: "Wrong service answered",
  });
  const ready = event("4", "Services ready");
  ready.phase = "ready";
  vi.spyOn(apiClient, "listCompositionEvents").mockResolvedValue({
    items: [
      event("1", "Starting"),
      event("2", "Container initializing"),
      failure,
      ready,
    ],
  });
  vi.spyOn(apiClient, "listActivity").mockResolvedValue({ items: [] });
  render(<CompositionHistory composition={composition} />);
  const failureMessage = await screen.findByText("Wrong service answered");
  expect(failureMessage.closest("details")).toBeNull();
  expect(screen.getByText("Services ready").closest("details")).toBeNull();
  expect(
    screen.getByText("Container initializing").closest("details")?.open,
  ).toBe(false);
  expect(
    screen.getByText("Intermediate deployment observations (1)"),
  ).toBeTruthy();
});
