import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiClient } from "../../lib/api-client";
import { Composition, Recipe } from "../../types/api";
import { RecipesView } from "./RecipesView";

const refreshAll = vi.fn();
const composition: Composition = {
  id: "cmp-1",
  project: "demo",
  baseline: "staging",
  baseline_revision: "demo-v1",
  name: "source",
  generation: 1,
  observed_generation: 1,
  phase: "ready",
  overrides: {
    "service-b": {
      image: `registry.example.com/service-b@sha256:${"a".repeat(64)}`,
    },
  },
  components: {},
  conditions: [],
  latest_operation: { id: "op-1", kind: "create", status: "succeeded" },
  endpoints: {
    public: { url: "http://cmp-1.envy.localhost:8080", ready: true },
  },
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  expires_at: "2026-01-02T00:00:00Z",
};
const recipe: Recipe = {
  api_version: "envy/recipe-v1",
  project: "demo",
  baseline: "staging",
  baseline_revision: "demo-v1",
  ttl: "8h",
  overrides: composition.overrides,
  frontends: [],
};

vi.mock("../../context/ApiContext", () => ({
  useEnvyApi: () => ({
    compositions: [composition],
    isDemoMode: false,
    refreshAll,
  }),
}));

afterEach(cleanup);
beforeEach(() => {
  refreshAll.mockReset().mockResolvedValue(undefined);
  vi.spyOn(apiClient, "validateRecipe").mockResolvedValue(recipe);
  vi.spyOn(apiClient, "recreateRecipe").mockResolvedValue({
    composition: { ...composition, id: "cmp-2", name: "recreated" },
    bindings: [],
    binding_errors: ["web: revision is not published yet"],
  });
});

describe("RecipesView", () => {
  it("validates a portable recipe and preserves its recreate key for partial-failure retries", async () => {
    const user = userEvent.setup();
    render(<RecipesView />);
    await user.type(
      screen.getByLabelText("Recipe JSON"),
      JSON.stringify(recipe),
    );
    await user.click(screen.getByRole("button", { name: "Validate" }));
    expect(
      await screen.findByText("Recipe is valid and portable."),
    ).toBeTruthy();

    await user.type(
      screen.getByPlaceholderText("New composition name"),
      "recreated",
    );
    await user.click(screen.getByRole("button", { name: "Recreate" }));
    expect(
      await screen.findByText(/some frontend bindings need retry/i),
    ).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Recreate" }));
    await waitFor(() =>
      expect(apiClient.recreateRecipe).toHaveBeenCalledTimes(2),
    );
    const firstKey = vi.mocked(apiClient.recreateRecipe).mock.calls[0][2];
    const retryKey = vi.mocked(apiClient.recreateRecipe).mock.calls[1][2];
    expect(retryKey).toBe(firstKey);
    expect(refreshAll).toHaveBeenCalledTimes(2);
  });

  it("shows invalid JSON inline and does not call the API", async () => {
    const user = userEvent.setup();
    render(<RecipesView />);
    await user.type(screen.getByLabelText("Recipe JSON"), "not json");
    await user.click(screen.getByRole("button", { name: "Validate" }));
    expect(screen.getByRole("status").textContent).toContain("valid JSON");
    expect(apiClient.validateRecipe).not.toHaveBeenCalled();
  });
});
