import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Composition } from "../../types/api";
import { UpdateCompositionDialog } from "./UpdateCompositionDialog";

const updateComposition = vi.fn();
afterEach(() => cleanup());
vi.mock("../../context/ApiContext", () => ({
  useEnvyApi: () => ({
    updateComposition,
    components: [{ id: "service-b", project: "demo", profile: "http-small" }],
  }),
}));
vi.mock("./RevisionPicker", () => ({
  selectedOverride: (value: string) =>
    value.startsWith("build:")
      ? { build_id: value.slice(6) }
      : { image: value },
  RevisionPicker: ({
    component,
    value,
    onChange,
  }: {
    component: string;
    value: string;
    onChange: (value: string) => void;
  }) => (
    <input
      aria-label={`${component} selection`}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  ),
}));

function composition(generation = 1): Composition {
  return {
    id: "cmp-1",
    project: "demo",
    baseline: "staging",
    baseline_revision: "rev-1",
    name: "preview",
    generation,
    observed_generation: generation,
    phase: "ready",
    verification_level: "routing",
    overrides: {
      "service-b": {
        build_id: "a".repeat(64),
        image: "ghcr.io/demo/service-b@sha256:abc",
      },
    },
    components: {
      "service-b": {
        source: "override",
        status: "ready",
        image: "ghcr.io/demo/service-b@sha256:abc",
      },
    },
    conditions: [],
    latest_operation: { id: "op", kind: "update", status: "succeeded" },
    endpoints: {
      public: { url: "http://cmp-1.envy.localhost:8080", ready: true },
    },
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    expires_at: "2026-01-02T00:00:00Z",
  };
}

describe("UpdateCompositionDialog", () => {
  beforeEach(() =>
    updateComposition.mockReset().mockResolvedValue(composition(2)),
  );
  it("preserves the current published build", async () => {
    render(
      <UpdateCompositionDialog
        composition={composition()}
        open
        onOpenChange={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        (screen.getByLabelText("service-b selection") as HTMLInputElement)
          .value,
      ).toBe(`build:${"a".repeat(64)}`),
    );
    fireEvent.submit(
      screen
        .getByRole("button", { name: /Deploy Generation 2/ })
        .closest("form")!,
    );
    await waitFor(() =>
      expect(updateComposition).toHaveBeenCalledWith("cmp-1", {
        expected_generation: 1,
        overrides: { "service-b": { build_id: "a".repeat(64) } },
      }),
    );
  });
  it("does not erase a draft when polling advances the displayed composition", async () => {
    const view = render(
      <UpdateCompositionDialog
        composition={composition()}
        open
        onOpenChange={() => {}}
      />,
    );
    const input = await screen.findByLabelText("service-b selection");
    fireEvent.change(input, { target: { value: "envy/service-b:draft" } });
    view.rerender(
      <UpdateCompositionDialog
        composition={composition(2)}
        open
        onOpenChange={() => {}}
      />,
    );
    expect(
      (screen.getByLabelText("service-b selection") as HTMLInputElement).value,
    ).toBe("envy/service-b:draft");
    expect(screen.getByText(/based on generation 1/)).toBeTruthy();
  });
});
