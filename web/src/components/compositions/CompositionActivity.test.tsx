import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { apiClient } from "../../lib/api-client";
import { Composition } from "../../types/api";
import { CompositionActivity } from "./CompositionActivity";

vi.mock("../../context/ApiContext", () => ({
  useEnvyApi: () => ({ isDemoMode: false, serverUrl: "", token: "" }),
}));

const composition = {
  id: "cmp-1",
  project: "demo",
  baseline: "staging",
  baseline_revision: "demo-v1",
  name: "preview",
  generation: 2,
  observed_generation: 2,
  phase: "ready",
  overrides: {},
  components: {},
  conditions: [],
  latest_operation: { id: "op-2", kind: "update", status: "succeeded" },
  endpoints: {
    public: { url: "http://cmp-1.envy.localhost:8080", ready: true },
  },
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  expires_at: "2026-01-02T00:00:00Z",
} as Composition;

afterEach(cleanup);

describe("CompositionActivity", () => {
  it("loads actor and channel attribution for the selected composition", async () => {
    vi.spyOn(apiClient, "listActivity").mockResolvedValue({
      items: [
        {
          id: "2",
          occurred_at: "2026-01-01T01:00:00Z",
          actor: { kind: "human", id: "alice", display_name: "Alice" },
          channel: "web",
          action: "composition.update",
          outcome: "accepted",
          resource_type: "composition",
          resource_id: "cmp-1",
          generation_from: 1,
          generation_to: 2,
        },
      ],
    });
    const user = userEvent.setup();
    render(<CompositionActivity composition={composition} />);
    await user.click(screen.getByRole("button", { name: "Load activity" }));
    expect(await screen.findByText(/Alice via web/)).toBeTruthy();
    expect(screen.getByText(/generation 1 → 2/)).toBeTruthy();
    expect(apiClient.listActivity).toHaveBeenCalledWith(
      { resource_id: "cmp-1", after: "" },
      expect.any(AbortSignal),
    );
  });

  it("surfaces loading failures inline", async () => {
    vi.spyOn(apiClient, "listActivity").mockRejectedValue(
      new Error("history offline"),
    );
    const user = userEvent.setup();
    render(<CompositionActivity composition={composition} />);
    await user.click(screen.getByRole("button", { name: "Load activity" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "history offline",
    );
  });
});
