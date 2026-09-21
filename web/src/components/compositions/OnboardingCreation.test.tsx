import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CreateCompositionView } from "./CreateCompositionView";
import { apiClient, ApiRequestError } from "../../lib/api-client";
const context = vi.hoisted(() => ({
  isDemoMode: false,
  createComposition: vi.fn(),
  installation: { default_ttl: "1h", max_ttl: "4h" },
  projects: [
    { id: "demo", name: "Demo" },
    { id: "shop", name: "Shop" },
  ],
  baselines: [
    { project: "demo", id: "staging", components: {} },
    {
      project: "shop",
      id: "review",
      components: { pricing: { image: "pricing:v1" } },
    },
  ],
  components: [
    {
      id: "pricing",
      project: "shop",
      overridable: true,
      profile: "deployment",
    },
  ],
}));
vi.mock("../../context/ApiContext", () => ({ useEnvyApi: () => context }));
vi.mock("./RevisionPicker", () => ({
  selectedOverride: (value: string) =>
    value.startsWith("build:")
      ? { build_id: value.slice(6) }
      : { image: value.trim() },
  RevisionPicker: ({
    value,
    onChange,
  }: {
    value: string;
    onChange: (value: string) => void;
  }) => (
    <input
      aria-label="Image or build"
      value={value}
      onChange={(event) => onChange(event.target.value)}
    />
  ),
}));
const profile = {
  project: "shop",
  baseline: "review",
  component: "pricing",
  revision: 3,
  selection: {},
  source_uid: "uid",
  contract: "contract",
  dependencies: [],
};
beforeEach(() => {
  context.components[0].profile = "deployment";
  vi.spyOn(apiClient, "inspectPreviewProfile").mockResolvedValue(profile);
  context.createComposition.mockReset().mockResolvedValue({ id: "created" });
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
async function setup() {
  const success = vi.fn();
  const prepare = vi.fn();
  render(
    <CreateCompositionView
      open
      initialProject="shop"
      initialBaseline="review"
      onCancel={vi.fn()}
      onSuccess={success}
      onPrepare={prepare}
    />,
  );
  const user = userEvent.setup();
  expect((screen.getByLabelText("Project") as HTMLSelectElement).value).toBe(
    "shop",
  );
  expect((screen.getByLabelText("Baseline") as HTMLSelectElement).value).toBe(
    "review",
  );
  await user.type(screen.getByLabelText("Preview name"), "pricing-check");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  return { user, success, prepare };
}
it.each(["deployment", "deployment-composite"])(
  "%s requires a digest and includes the reviewed revision",
  async (kind) => {
    context.components[0].profile = kind;
    const { user, success } = await setup();
    await screen.findByText("Approved profile revision 3");
    await user.type(screen.getByLabelText("Image or build"), "pricing:latest");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(screen.getByRole("alert").textContent).toContain(
      "immutable image digest",
    );
    await user.clear(screen.getByLabelText("Image or build"));
    await user.type(screen.getByLabelText("Image or build"), "build:build-one");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(context.createComposition).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Create preview" }));
    await waitFor(() => expect(success).toHaveBeenCalledWith("created"));
    expect(context.createComposition).toHaveBeenCalledWith(
      expect.objectContaining({
        project: "shop",
        baseline: "review",
        overrides: { pricing: { build_id: "build-one" } },
        expected_preview_revisions: { pricing: 3 },
      }),
      expect.any(String),
    );
  },
);
it.each(["deployment", "deployment-composite"])(
  "%s blocks creation when approval is missing",
  async (kind) => {
    context.components[0].profile = kind;
    vi.mocked(apiClient.inspectPreviewProfile).mockRejectedValue(
      new ApiRequestError("not found", 404),
    );
    const { user, prepare } = await setup();
    await screen.findByText("Prepare this service before overriding it.");
    await user.type(screen.getByLabelText("Image or build"), "build:one");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(screen.getByRole("alert").textContent).toContain("Prepare pricing");
    await user.click(screen.getByRole("button", { name: "Prepare pricing" }));
    expect(prepare).toHaveBeenCalledWith({
      project: "shop",
      baseline: "review",
    });
    expect(context.createComposition).not.toHaveBeenCalled();
  },
);
it("returns to review after a conflict and submits the refreshed revision only after another review", async () => {
  context.createComposition
    .mockRejectedValueOnce(
      new ApiRequestError("[conflict] preview profile revision changed", 409),
    )
    .mockResolvedValueOnce({ id: "created" });
  const { user } = await setup();
  await screen.findByText("Approved profile revision 3");
  await user.type(screen.getByLabelText("Image or build"), "build:one");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  vi.mocked(apiClient.inspectPreviewProfile).mockResolvedValue({
    ...profile,
    revision: 4,
  });
  await user.click(screen.getByRole("button", { name: "Create preview" }));
  await screen.findByText("Approved profile revision 4");
  expect(
    (screen.getByLabelText("Image or build") as HTMLInputElement).value,
  ).toBe("build:one");
  expect(context.createComposition).toHaveBeenCalledTimes(1);
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.click(screen.getByRole("button", { name: "Create preview" }));
  expect(
    context.createComposition.mock.calls[1][0].expected_preview_revisions,
  ).toEqual({ pricing: 4 });
  expect(context.createComposition.mock.calls[1][1]).not.toBe(
    context.createComposition.mock.calls[0][1],
  );
});

it("ignores a create response after the connection-scoped view is unmounted", async () => {
  let finish!: (value: { id: string }) => void;
  context.createComposition.mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const { user, success } = await setup();
  await screen.findByText("Approved profile revision 3");
  await user.type(screen.getByLabelText("Image or build"), "build:one");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.click(screen.getByRole("button", { name: "Create preview" }));
  cleanup();
  finish({ id: "old-installation-preview" });
  await Promise.resolve();
  expect(success).not.toHaveBeenCalled();
});
