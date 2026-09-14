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
    components: [
      {
        id: "service-a",
        project: "demo",
        profile: "http-small",
        overridable: true,
      },
      {
        id: "service-b",
        project: "demo",
        profile: "http-small",
        overridable: true,
      },
    ],
    baselines: [
      {
        id: "staging",
        project: "demo",
        components: {
          "service-a": { image: "v1", port: 8080, service_host: "service-a" },
          "service-b": { image: "v1", port: 8080, service_host: "service-b" },
        },
      },
    ],
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
  it("allows a composition to inherit the complete baseline", async () => {
    render(
      <UpdateCompositionDialog
        composition={composition()}
        open
        onOpenChange={() => {}}
      />,
    );
    await screen.findByLabelText("service-b selection");
    fireEvent.click(screen.getByRole("checkbox", { name: "service-b" }));
    expect(
      screen.getAllByText(/Complete baseline inheritance/).length,
    ).toBeGreaterThan(0);
    fireEvent.submit(
      screen
        .getByRole("button", { name: /Deploy Generation 2/ })
        .closest("form")!,
    );
    await waitFor(() =>
      expect(updateComposition).toHaveBeenCalledWith("cmp-1", {
        expected_generation: 1,
        overrides: {},
      }),
    );
  });
  it("can add another approved component to the desired selection", async () => {
    render(
      <UpdateCompositionDialog
        composition={composition()}
        open
        onOpenChange={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("checkbox", { name: "service-a" }));
    fireEvent.change(await screen.findByLabelText("service-a selection"), {
      target: { value: "envy/service-a:v2" },
    });
    fireEvent.submit(
      screen
        .getByRole("button", { name: /Deploy Generation 2/ })
        .closest("form")!,
    );
    await waitFor(() =>
      expect(updateComposition).toHaveBeenCalledWith("cmp-1", {
        expected_generation: 1,
        overrides: {
          "service-a": { image: "envy/service-a:v2" },
          "service-b": { build_id: "a".repeat(64) },
        },
      }),
    );
  });
});
