import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CompositionDiagnostics } from "./CompositionDiagnostics";
import { apiClient } from "../../lib/api-client";
import { INITIAL_MOCK_COMPOSITIONS } from "../../lib/mock-data";

vi.mock("../../context/ApiContext", () => ({
  useEnvyApi: () => ({ isDemoMode: false }),
}));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
it("selects supporting and previous logs and clears selection when switching to inherited traffic", async () => {
  const composition = {
    ...INITIAL_MOCK_COMPOSITIONS[0],
    overrides: { "service-b": { image: "example/app:fixture" } },
  };
  const read = vi.spyOn(apiClient, "getComponentLogs").mockResolvedValue({
    id: composition.id,
    project: composition.project,
    component: "service-b",
    source: "override",
    composition_filtered: false,
    message: "Selected approved container",
    streams: [],
    truncated: false,
    partial: false,
  });
  const { rerender } = render(
    <CompositionDiagnostics composition={composition} />,
  );
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Container"), "bootstrap");
  await user.click(screen.getByLabelText("Previous container instance"));
  await user.click(screen.getByRole("button", { name: "Read logs" }));
  await screen.findByText("Selected approved container");
  expect(read).toHaveBeenLastCalledWith(
    composition.id,
    "service-b",
    expect.any(AbortSignal),
    "bootstrap",
    true,
    { tail_lines: 200, max_bytes: 65536, since_seconds: 0 },
  );
  rerender(
    <CompositionDiagnostics composition={{ ...composition, overrides: {} }} />,
  );
  expect(screen.queryByLabelText("Container")).toBeNull();
  expect(screen.queryByText("Selected approved container")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Read logs" }));
  await screen.findByText("Selected approved container");
  expect(read).toHaveBeenLastCalledWith(
    composition.id,
    "service-b",
    expect.any(AbortSignal),
    "",
    true,
    { tail_lines: 200, max_bytes: 65536, since_seconds: 0 },
  );
});
