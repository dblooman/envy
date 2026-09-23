import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApplicationOnboarding } from "./ApplicationOnboarding";
import { CatalogView } from "./CatalogView";
import { ApiRequestError, apiClient } from "../../lib/api-client";
import type {
  CatalogManifest,
  CatalogReport,
  PreviewReport,
} from "../../types/api";

const context = vi.hoisted(() => ({
  projects: [],
  components: [],
  baselines: [],
  refreshAll: vi.fn().mockResolvedValue(undefined),
  isDemoMode: false,
  serverStatus: "connected",
  installation: { id: "one" },
  session: { principal: { id: "alice" } },
}));
vi.mock("../../context/ApiContext", () => ({ useEnvyApi: () => context }));
const baseline = {
  id: "staging",
  project: "shop",
  revision: "v1",
  endpoint: "http://shop.example.test",
  components: {
    pricing: {
      service_host: "pricing.shop.svc.cluster.local",
      port: 8080,
      image: "pricing:v1",
    },
  },
};
const component = {
  id: "pricing",
  project: "shop",
  protocol: "http",
  port: 8080,
  profile: "deployment" as const,
  overridable: true,
};
function response(configuration: CatalogManifest): CatalogReport {
  return {
    configuration,
    checks: [
      { type: "ConfigurationValid", status: true, message: "Validated" },
    ],
    warnings: ["Reachability only"],
    applied: false,
  };
}
function discover(): PreviewReport {
  return {
    source: {
      namespace: "shop",
      deployment: "pricing",
      container: "app",
      uid: "uid",
      resource_version: "1",
      generation: 1,
    },
    selection: { deployment: "pricing", container: "app" },
    inspection: "inspection-one",
    contract: "contract",
    configuration: {},
    dependencies: [],
    blockers: [],
    warnings: [],
    source_read_rules: [],
  };
}
beforeEach(() => {
  context.projects = [];
  context.components = [];
  context.baselines = [];
  context.isDemoMode = false;
  context.serverStatus = "connected";
  context.installation = { id: "one" };
  vi.spyOn(apiClient, "validateCatalog").mockImplementation(async (manifest) =>
    response(manifest),
  );
  vi.spyOn(apiClient, "applyCatalog").mockImplementation(async (manifest) => ({
    ...response(manifest),
    applied: true,
  }));
  vi.spyOn(apiClient, "inspectPreviewProfile").mockRejectedValue(
    new ApiRequestError("not found", 404),
  );
  vi.spyOn(apiClient, "discoverPreviewProfile").mockResolvedValue(discover());
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
async function describe() {
  const user = userEvent.setup();
  for (const [label, value] of Object.entries({
    "Project ID": "shop",
    "Project name": "Shop",
    "Baseline endpoint": "http://shop.example.test",
    Namespace: "shop",
    Gateway: "ingress",
    "Entry component ID": "pricing",
    "Component ID 1": "pricing",
    "Service name 1": "pricing",
    "Baseline image 1": "pricing:v1",
  }))
    fireEvent.change(screen.getByLabelText(label), { target: { value } });
  await user.click(
    screen.getByRole("button", { name: "Continue to configuration" }),
  );
  return user;
}
it("registers deployment configuration without duplicate workload fields and explicitly hands off creation", async () => {
  const create = vi.fn();
  render(<ApplicationOnboarding onCreate={create} onClose={vi.fn()} />);
  const user = await describe();
  expect(document.activeElement?.textContent).toBe(
    "Configure and register the baseline",
  );
  await user.click(
    screen.getByRole("button", { name: "Validate configuration" }),
  );
  const manifest = vi.mocked(apiClient.validateCatalog).mock.calls[0][0];
  expect(manifest.components[0]).toEqual(component);
  expect(manifest.baseline.components.pricing.service_host).toBe(
    "pricing.shop.svc.cluster.local",
  );
  expect(manifest.baseline.verification).toEqual({
    kind: "http",
    path: "/",
    expected_status: 200,
  });
  expect(apiClient.applyCatalog).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Register baseline" }));
  await screen.findByText("Preparation required");
  expect(create).not.toHaveBeenCalled();
  await user.click(
    screen.getByRole("button", { name: "Continue to first preview" }),
  );
  await user.click(
    screen.getByRole("button", { name: "Choose preview changes" }),
  );
  expect(create).toHaveBeenCalledWith({ project: "shop", baseline: "staging" });
});
it("invalidates validation on edits and retains manual settings through failed registration retries", async () => {
  vi.mocked(apiClient.applyCatalog).mockRejectedValueOnce(
    new ApiRequestError("[conflict] immutable entry", 409),
  );
  render(<ApplicationOnboarding onCreate={vi.fn()} onClose={vi.fn()} />);
  const user = await describe();
  await user.selectOptions(
    screen.getByLabelText("Profile for pricing"),
    "http-small",
  );
  await user.click(screen.getByRole("button", { name: "Add Environment 1" }));
  await user.type(screen.getByLabelText("Environment 1 name 1"), "API_URL");
  await user.type(
    screen.getByLabelText("Environment 1 value 1"),
    "http://api.shop.svc.cluster.local",
  );
  await user.click(
    screen.getByRole("button", { name: "Validate configuration" }),
  );
  await screen.findByRole("button", { name: "Register baseline" });
  await user.type(screen.getByLabelText("Health path 1"), "/live");
  expect(
    screen.queryByRole("button", { name: "Register baseline" }),
  ).toBeNull();
  await user.click(
    screen.getByRole("button", { name: "Validate configuration" }),
  );
  await user.click(screen.getByRole("button", { name: "Register baseline" }));
  expect(screen.getByRole("alert").textContent).toContain("immutable entry");
  await user.click(screen.getByRole("button", { name: "Register baseline" }));
  await screen.findByText(/Manual HTTP profile registered/);
  expect(apiClient.applyCatalog).toHaveBeenCalledTimes(2);
  expect(vi.mocked(apiClient.applyCatalog).mock.calls[0]).toEqual(
    vi.mocked(apiClient.applyCatalog).mock.calls[1],
  );
  expect(
    vi.mocked(apiClient.applyCatalog).mock.calls[1][0].components[0].env,
  ).toEqual({ API_URL: "http://api.shop.svc.cluster.local" });
});
it("submits mixed profiles and rejects duplicate component IDs before making a request", async () => {
  render(<ApplicationOnboarding onCreate={vi.fn()} onClose={vi.fn()} />);
  const user = await describe();
  await user.click(screen.getByRole("button", { name: "Back" }));
  await user.click(screen.getByRole("button", { name: "Add component" }));
  for (const [label, value] of Object.entries({
    "Component ID 2": "pricing",
    "Service name 2": "gateway",
    "Baseline image 2": "gateway:v1",
  }))
    fireEvent.change(screen.getByLabelText(label), { target: { value } });
  await user.click(
    screen.getByRole("button", { name: "Continue to configuration" }),
  );
  await user.click(
    screen.getByRole("button", { name: "Validate configuration" }),
  );
  expect(screen.getByRole("alert").textContent).toContain("unique");
  expect(apiClient.validateCatalog).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Back" }));
  fireEvent.change(screen.getByLabelText("Component ID 2"), {
    target: { value: "gateway" },
  });
  await user.click(
    screen.getByRole("button", { name: "Continue to configuration" }),
  );
  await user.selectOptions(
    screen.getByLabelText("Profile for gateway"),
    "http-small",
  );
  await user.click(
    screen.getByRole("button", { name: "Validate configuration" }),
  );
  expect(
    vi
      .mocked(apiClient.validateCatalog)
      .mock.calls[0][0].components.map((c) => c.profile),
  ).toEqual(["deployment", "http-small"]);
});
it("resumes saved preparation without registering again", async () => {
  Object.assign(context, {
    projects: [{ id: "shop", name: "Shop" }],
    baselines: [baseline],
    components: [component],
  });
  render(
    <ApplicationOnboarding
      resume={{ project: "shop", baseline: "staging" }}
      onCreate={vi.fn()}
      onClose={vi.fn()}
    />,
  );
  await screen.findByText("Preparation required");
  expect(apiClient.validateCatalog).not.toHaveBeenCalled();
  expect(apiClient.applyCatalog).not.toHaveBeenCalled();
});
it("resumes a partial private draft after remount and surfaces revision conflicts", async () => {
  let stored: import("../../types/api").OnboardingDraft | undefined;
  vi.spyOn(apiClient, "saveOnboardingDraft").mockImplementation(
    async (draft) => {
      stored = { ...draft, revision: 1, updated_at: new Date().toISOString() };
      return stored;
    },
  );
  vi.spyOn(apiClient, "getOnboardingDraft").mockImplementation(
    async () => stored!,
  );
  const view = render(
    <ApplicationOnboarding onCreate={vi.fn()} onClose={vi.fn()} />,
  );
  fireEvent.change(screen.getByLabelText("Project ID"), {
    target: { value: "shop" },
  });
  fireEvent.change(screen.getByLabelText("Project name"), {
    target: { value: "Shop" },
  });
  fireEvent.change(screen.getByLabelText("Namespace"), {
    target: { value: "test-ns" },
  });
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Save preparation" }));
  expect(stored?.revision).toBe(1);
  view.unmount();
  render(<ApplicationOnboarding onCreate={vi.fn()} onClose={vi.fn()} />);
  fireEvent.change(screen.getByLabelText("Project ID"), {
    target: { value: "shop" },
  });
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Load saved preparation" }));
  expect((screen.getByLabelText("Namespace") as HTMLInputElement).value).toBe(
    "test-ns",
  );
  vi.mocked(apiClient.saveOnboardingDraft).mockRejectedValueOnce(
    new ApiRequestError("[conflict] changed", 409),
  );
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Save preparation" }));
  expect(screen.getByRole("alert").textContent).toContain("changed");
});
it("loads a componentless draft saved by another client", async () => {
  vi.spyOn(apiClient, "getOnboardingDraft").mockResolvedValue({
    project: "shop",
    revision: 2,
    stage: 0,
    configuration: {
      api_version: "envy/v1",
      project: { id: "shop", name: "Shop" },
      components: null,
      baseline: {
        id: "staging",
        project: "shop",
        revision: "v1",
        endpoint: "",
        routing: {},
        verification: {},
        components: null,
      },
    } as unknown as CatalogManifest,
  });
  render(<ApplicationOnboarding onCreate={vi.fn()} onClose={vi.fn()} />);
  fireEvent.change(screen.getByLabelText("Project ID"), {
    target: { value: "shop" },
  });
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Load saved preparation" }));
  expect(screen.getByLabelText("Component ID 1")).toBeTruthy();
  expect(screen.getByText(/Resumed preparation revision 2/)).toBeTruthy();
});
it("clears drafts and ignores late validation when the installation changes", async () => {
  let resolve!: (report: CatalogReport) => void;
  vi.mocked(apiClient.validateCatalog).mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  const view = render(<CatalogView />);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Onboard application" }));
  await describe();
  await user.click(
    screen.getByRole("button", { name: "Validate configuration" }),
  );
  const manifest = vi.mocked(apiClient.validateCatalog).mock.calls[0][0];
  context.installation = { id: "two" };
  view.rerender(<CatalogView />);
  resolve(response(manifest));
  await user.click(screen.getByRole("button", { name: "Onboard application" }));
  expect((screen.getByLabelText("Project ID") as HTMLInputElement).value).toBe(
    "",
  );
  expect(
    screen.queryByRole("button", { name: "Register baseline" }),
  ).toBeNull();
});
it("shows demo availability without making registration requests", async () => {
  context.isDemoMode = true;
  render(<CatalogView />);
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Onboard application" }));
  expect(screen.getByText(/Connect to a live installation/)).toBeTruthy();
  expect(apiClient.validateCatalog).not.toHaveBeenCalled();
});

it("requires fresh discovery after replacements and handles approval conflicts without losing edits", async () => {
  Object.assign(context, { baselines: [baseline], components: [component] });
  const approval = vi
    .spyOn(apiClient, "approvePreviewProfile")
    .mockRejectedValueOnce(
      new ApiRequestError("[conflict] source changed", 409),
    )
    .mockResolvedValue({
      project: "shop",
      baseline: "staging",
      component: "pricing",
      revision: 2,
      selection: {},
      source_uid: "uid",
      contract: "contract",
      dependencies: [],
    });
  render(
    <ApplicationOnboarding
      resume={{ project: "shop", baseline: "staging" }}
      onCreate={vi.fn()}
      onClose={vi.fn()}
    />,
  );
  const user = userEvent.setup();
  await screen.findByText("Preparation required");
  await user.click(
    screen.getByRole("button", { name: "Discover configuration" }),
  );
  const confirm = () =>
    screen.getByRole("checkbox", { name: /I have checked/ });
  await user.click(confirm());
  await user.click(
    screen.getByRole("button", { name: "Add Environment replacement" }),
  );
  expect(
    screen.queryByRole("button", { name: "Approve inspected configuration" }),
  ).toBeNull();
  await user.type(
    screen.getByLabelText("Environment replacement name 1"),
    "API_URL",
  );
  await user.type(
    screen.getByLabelText("Environment replacement value 1"),
    "http://api.shop.svc.cluster.local",
  );
  vi.mocked(apiClient.discoverPreviewProfile).mockImplementation(
    async (_p, _b, _c, selection) => ({ ...discover(), selection }),
  );
  await user.click(
    screen.getByRole("button", { name: "Discover configuration" }),
  );
  await user.click(confirm());
  await user.click(
    screen.getByRole("button", { name: "Approve inspected configuration" }),
  );
  await screen.findByText(/source changed/);
  expect(
    (
      screen.getByLabelText(
        "Environment replacement value 1",
      ) as HTMLInputElement
    ).value,
  ).toContain("api.shop");
  await user.click(
    screen.getByRole("button", { name: "Discover configuration" }),
  );
  expect((confirm() as HTMLInputElement).checked).toBe(false);
  await user.click(confirm());
  await user.click(
    screen.getByRole("button", { name: "Approve inspected configuration" }),
  );
  await screen.findByText("Approved revision 2.");
  expect(approval).toHaveBeenLastCalledWith(
    "shop",
    "staging",
    "pricing",
    expect.objectContaining({
      inspection: "inspection-one",
      expected_revision: 0,
      confirm_connectivity: true,
    }),
  );
});
it("keeps discovery blockers visible and prevents approval even when confirmed", async () => {
  Object.assign(context, { baselines: [baseline], components: [component] });
  vi.mocked(apiClient.discoverPreviewProfile).mockResolvedValue({
    ...discover(),
    blockers: ["Grant named Secret read access"],
    source_read_rules: [
      {
        resources: ["secrets"],
        resourceNames: ["pricing-config"],
        verbs: ["get"],
      },
    ],
  });
  render(
    <ApplicationOnboarding
      resume={{ project: "shop", baseline: "staging" }}
      onCreate={vi.fn()}
      onClose={vi.fn()}
    />,
  );
  const user = userEvent.setup();
  await screen.findByText("Preparation required");
  await user.click(
    screen.getByRole("button", { name: "Discover configuration" }),
  );
  const section = screen.getByRole("region", { name: "Prepare pricing" });
  expect(within(section).getByRole("alert").textContent).toContain(
    "Grant named Secret",
  );
  await user.click(screen.getByRole("checkbox", { name: /I have checked/ }));
  expect(
    (
      screen.getByRole("button", {
        name: "Approve inspected configuration",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(screen.getByText(/Arrange these named permissions/)).toBeTruthy();
});
it("treats permission failures on profile lookup as unknown, not an absent approval", async () => {
  Object.assign(context, { baselines: [baseline], components: [component] });
  vi.mocked(apiClient.inspectPreviewProfile).mockRejectedValue(
    new ApiRequestError("Forbidden", 403),
  );
  render(
    <ApplicationOnboarding
      resume={{ project: "shop", baseline: "staging" }}
      onCreate={vi.fn()}
      onClose={vi.fn()}
    />,
  );
  await waitFor(() =>
    expect(screen.getByRole("alert").textContent).toContain("Forbidden"),
  );
  expect(screen.queryByText("Preparation required")).toBeNull();
});

it("reuses an existing project even when its ID is new", async () => {
  Object.assign(context, {
    projects: [{ id: "new", name: "Existing project" }],
  });
  render(<ApplicationOnboarding onCreate={vi.fn()} onClose={vi.fn()} />);
  const user = await describe();
  await user.click(screen.getByRole("button", { name: "Back" }));
  await user.selectOptions(
    screen.getByRole("combobox", { name: "Project" }),
    "new",
  );
  expect(screen.queryByLabelText("Project ID")).toBeNull();
  await user.click(
    screen.getByRole("button", { name: "Continue to configuration" }),
  );
  await user.click(
    screen.getByRole("button", { name: "Validate configuration" }),
  );
  expect(vi.mocked(apiClient.validateCatalog).mock.calls[0][0].project).toEqual(
    { id: "new", name: "Existing project" },
  );
});
