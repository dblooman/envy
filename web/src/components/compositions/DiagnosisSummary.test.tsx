import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { DiagnosisSummary } from "./DiagnosisSummary";
import { apiClient } from "../../lib/api-client";
import { INITIAL_MOCK_COMPOSITIONS } from "../../lib/mock-data";

vi.mock("../../context/ApiContext", () => ({
  useEnvyApi: () => ({ isDemoMode: false }),
}));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

it("shows independent blockers without claiming a unique cause", async () => {
  const c = INITIAL_MOCK_COMPOSITIONS[0];
  vi.spyOn(apiClient, "diagnosis").mockResolvedValue({
    composition: c.id,
    generation: c.generation,
    phase: "failed",
    observed_at: "2026-09-23T10:00:00Z",
    state: "blocked",
    verification: "failed",
    blockers: [
      {
        code: "workload_failed",
        scope: "component/api",
        message: "Image pull failed",
        next_step: "Inspect image access.",
        observed_at: "2026-09-23T10:00:00Z",
      },
      {
        code: "messaging_pending",
        scope: "messaging",
        message: "Subscription unavailable",
        next_step: "Inspect binding.",
        observed_at: "2026-09-23T10:00:00Z",
      },
    ],
    notes: [],
  });
  render(<DiagnosisSummary composition={c} />);
  expect(await screen.findByText(/2 observed blockers/)).toBeTruthy();
  expect(screen.getByText(/Image pull failed/)).toBeTruthy();
  expect(screen.getByText(/Subscription unavailable/)).toBeTruthy();
  expect(screen.getByText(/may be independent/)).toBeTruthy();
});

it("reports a failed diagnosis read without inventing a healthy state", async () => {
  vi.spyOn(apiClient, "diagnosis").mockRejectedValue(new Error("offline"));
  render(<DiagnosisSummary composition={INITIAL_MOCK_COMPOSITIONS[0]} />);
  expect(await screen.findByRole("status")).toHaveProperty(
    "textContent",
    "Diagnosis could not be loaded. Inspect conditions and events.",
  );
});
