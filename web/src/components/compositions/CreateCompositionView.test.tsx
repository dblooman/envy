import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { CreateCompositionView } from "./CreateCompositionView";
import { durationSeconds, lifetimeOptions } from "../../lib/duration";
import { apiClient } from "../../lib/api-client";

const createComposition = vi.fn();
const baseline = {
  id: "staging",
  project: "demo",
  components: { "service-b": { image: "v1" } },
};
const catalog = {
  createComposition,
  projects: [{ id: "demo", name: "Demo" }],
  baselines: [baseline],
  components: [{ id: "service-b", project: "demo", overridable: true }],
  isDemoMode: false,
  installation: { default_ttl: "2h0m0s", max_ttl: "6h0m0s" },
};
vi.mock("../../context/ApiContext", () => ({ useEnvyApi: () => catalog }));
vi.mock("./RevisionPicker", () => ({
  selectedOverride: (value: string) =>
    value.startsWith("build:")
      ? { build_id: value.slice(6) }
      : { image: value.trim() },
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
      aria-label={`${component} revision`}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  ),
}));
afterEach(cleanup);
beforeEach(() => {
  createComposition.mockReset().mockResolvedValue({ id: "created" });
  delete (catalog.installation as { id?: string }).id;
  vi.spyOn(apiClient, "planComposition").mockResolvedValue({
    ready: true,
    project: "demo",
    baseline: "staging",
    installation: "test",
    verification_level: "reachability",
    resource_demand: "estimate",
    message_isolation: false,
    selected: [],
    inherited: {},
    blockers: [],
  });
});

it("invalidates a reviewed plan when the installation changes", async () => {
  const user = userEvent.setup();
  Object.assign(catalog.installation, { id: "one" });
  const view = render(
    <CreateCompositionView open onCancel={vi.fn()} onSuccess={vi.fn()} />,
  );
  await user.type(screen.getByLabelText("Preview name"), "scope-check");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.type(screen.getByLabelText("service-b revision"), "build:one");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(
    (
      (await screen.findByRole("button", {
        name: "Create preview",
      })) as HTMLButtonElement
    ).disabled,
  ).toBe(false);
  Object.assign(catalog.installation, { id: "two" });
  view.rerender(
    <CreateCompositionView open onCancel={vi.fn()} onSuccess={vi.fn()} />,
  );
  expect(
    (
      screen.getByRole("button", {
        name: "Create preview",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(createComposition).not.toHaveBeenCalled();
});
it("closes a reviewed draft without creating a preview", async () => {
  const user = userEvent.setup();
  const cancel = vi.fn();
  render(<CreateCompositionView open onCancel={cancel} onSuccess={vi.fn()} />);
  await user.type(screen.getByLabelText("Preview name"), "cancelled-review");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.type(screen.getByLabelText("service-b revision"), "build:one");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(apiClient.planComposition).toHaveBeenCalledTimes(1);
  await user.click(screen.getByRole("button", { name: "Close draft" }));
  expect(cancel).toHaveBeenCalledTimes(1);
  expect(createComposition).not.toHaveBeenCalled();
});
afterEach(() => vi.restoreAllMocks());

it("respects server duration syntax and presents options within the installation maximum", () => {
  expect(durationSeconds("6h30m0s")).toBe(23400);
  expect(durationSeconds("500ms")).toBe(0.5);
  expect(durationSeconds("forever")).toBeNull();
  expect(lifetimeOptions("6h0m0s")).toEqual(["1h", "4h", "6h0m0s"]);
  render(<CreateCompositionView open onCancel={vi.fn()} onSuccess={vi.fn()} />);
  expect(
    screen.getByRole("option", {
      name: "Installation default (2h0m0s)",
      hidden: true,
    }),
  ).toBeTruthy();
  expect(
    screen.queryByRole("option", { name: "24h", hidden: true }),
  ).toBeNull();
});

it("validates steps and submits complete baseline inheritance without an invented lifetime", async () => {
  const user = userEvent.setup();
  const success = vi.fn();
  render(<CreateCompositionView open onCancel={vi.fn()} onSuccess={success} />);
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(screen.getByRole("alert").textContent).toContain(
    "Preview name is required",
  );
  await user.type(screen.getByLabelText("Preview name"), "baseline-only");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(screen.getByRole("alert").textContent).toContain("published build");
  await user.click(screen.getByRole("checkbox", { name: "service-b" }));
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(createComposition).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Create preview" }));
  await waitFor(() => expect(success).toHaveBeenCalledWith("created"));
  expect(createComposition).toHaveBeenCalledWith(
    {
      message_isolation: false,
      project: "demo",
      baseline: "staging",
      name: "baseline-only",
      overrides: {},
    },
    expect.any(String),
  );
});

it("preserves drafts while hidden and uses the same idempotency key for identical retries", async () => {
  createComposition
    .mockRejectedValueOnce(new Error("Connection interrupted"))
    .mockResolvedValueOnce({ id: "created" });
  const user = userEvent.setup();
  const onCancel = vi.fn();
  const onSuccess = vi.fn();
  const view = render(
    <CreateCompositionView open onCancel={onCancel} onSuccess={onSuccess} />,
  );
  await user.type(screen.getByLabelText("Preview name"), "build-review");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.type(
    screen.getByLabelText("service-b revision"),
    "build:published-id",
  );
  view.rerender(
    <CreateCompositionView
      open={false}
      onCancel={onCancel}
      onSuccess={onSuccess}
    />,
  );
  view.rerender(
    <CreateCompositionView open onCancel={onCancel} onSuccess={onSuccess} />,
  );
  expect(
    (screen.getByLabelText("service-b revision") as HTMLInputElement).value,
  ).toBe("build:published-id");
  await user.click(
    screen.getByText("Advanced configuration", { selector: "summary" }),
  );
  await user.selectOptions(screen.getByLabelText("Lifetime"), "4h");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.click(screen.getByRole("button", { name: "Create preview" }));
  expect(screen.getByRole("alert").textContent).toBe("Connection interrupted");
  await user.click(screen.getByRole("button", { name: "Create preview" }));
  expect(createComposition).toHaveBeenCalledTimes(2);
  expect(createComposition.mock.calls[0]).toEqual(
    createComposition.mock.calls[1],
  );
  expect(createComposition.mock.calls[0][0]).toEqual({
    message_isolation: false,
    project: "demo",
    baseline: "staging",
    name: "build-review",
    overrides: { "service-b": { build_id: "published-id" } },
    ttl: "4h",
  });
});

it("uses a new request identity after editing a failed request and prevents concurrent submission", async () => {
  createComposition.mockRejectedValueOnce(new Error("Image rejected"));
  const user = userEvent.setup();
  render(<CreateCompositionView open onCancel={vi.fn()} onSuccess={vi.fn()} />);
  await user.type(screen.getByLabelText("Preview name"), "image-review");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.type(screen.getByLabelText("service-b revision"), "example:v2");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.click(screen.getByRole("button", { name: "Create preview" }));
  await user.click(screen.getByRole("button", { name: "Back" }));
  await user.clear(screen.getByLabelText("service-b revision"));
  await user.type(screen.getByLabelText("service-b revision"), "example:v3");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  createComposition.mockImplementationOnce(() => new Promise(() => {}));
  const form = screen
    .getByRole("button", { name: "Create preview" })
    .closest("form")!;
  fireEvent.submit(form);
  fireEvent.submit(form);
  expect(createComposition).toHaveBeenCalledTimes(2);
  expect(createComposition.mock.calls[0][1]).not.toBe(
    createComposition.mock.calls[1][1],
  );
});

it("submits isolation independently of consumer overrides", async () => {
  const user = userEvent.setup();
  render(<CreateCompositionView open onCancel={vi.fn()} onSuccess={vi.fn()} />);
  await user.type(screen.getByLabelText("Preview name"), "capture-only");
  await user.click(
    screen.getByRole("checkbox", { name: /Isolate Pub\/Sub messages/ }),
  );
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.click(screen.getByRole("checkbox", { name: "service-b" }));
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.click(screen.getByRole("button", { name: "Create preview" }));
  await waitFor(() => expect(createComposition).toHaveBeenCalled());
  expect(createComposition.mock.calls[0][0]).toMatchObject({
    message_isolation: true,
    overrides: {},
  });
});

it("keeps creation blocked when the server plan reports a stale approval", async () => {
  vi.mocked(apiClient.planComposition).mockResolvedValueOnce({
    ready: false,
    project: "demo",
    baseline: "staging",
    installation: "test",
    verification_level: "unknown",
    resource_demand: "not_resolved",
    message_isolation: false,
    selected: [],
    inherited: {},
    blockers: [
      {
        code: "stale_approval",
        source: "source cluster",
        message: "Approval changed",
        next_action: "Approve again",
      },
    ],
  });
  const user = userEvent.setup();
  render(<CreateCompositionView open onCancel={vi.fn()} onSuccess={vi.fn()} />);
  await user.type(screen.getByLabelText("Preview name"), "stale-check");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.type(screen.getByLabelText("service-b revision"), "build:one");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(
    (
      (await screen.findByRole("button", {
        name: "Create preview",
      })) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(screen.getByText(/stale_approval/).textContent).toContain(
    "Approve again",
  );
  expect(createComposition).not.toHaveBeenCalled();
});
