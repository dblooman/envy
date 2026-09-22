import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AppContent, routeState } from "./App";
import { INITIAL_MOCK_COMPOSITIONS } from "./lib/mock-data";

const destroyComposition = vi.fn();
const data = {
  compositions: INITIAL_MOCK_COMPOSITIONS,
  projects: [],
  baselines: [],
  components: [],
  destroyComposition,
  createComposition: vi.fn(),
  loading: false,
  error: null,
  serverStatus: "demo",
  isDemoMode: true,
  session: null,
  installation: null,
  refreshAll: vi.fn(),
};
vi.mock("./context/ApiContext", () => ({
  useEnvyApi: () => data,
  ApiProvider: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("./context/ThemeContext", () => ({
  useTheme: () => ({ isDark: false, setTheme: vi.fn() }),
  ThemeProvider: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("./components/compositions/FrontendBindings", () => ({
  FrontendBindings: () => <div>Frontend bindings panel</div>,
}));
vi.mock("./components/compositions/CompositionRevisions", () => ({
  CompositionRevisions: () => <div>Revisions panel</div>,
}));
vi.mock("./components/compositions/CompositionDiagnostics", () => ({
  CompositionDiagnostics: () => <div>Diagnostics panel</div>,
}));
vi.mock("./components/compositions/CompositionHistory", () => ({
  CompositionHistory: () => <div>Preview activity panel</div>,
}));
vi.mock("./components/compositions/UpdateCompositionDialog", () => ({
  UpdateCompositionDialog: ({ open }: { open: boolean }) =>
    open ? <div role="dialog" aria-label="Update preview form" /> : null,
}));
afterEach(cleanup);
beforeEach(() => {
  window.history.replaceState({}, "", "/compositions");
  destroyComposition.mockReset().mockResolvedValue(undefined);
});

it("keeps encoded deep links compatible and renders the detail page directly", async () => {
  const comp = INITIAL_MOCK_COMPOSITIONS[0];
  window.history.replaceState(
    {},
    "",
    `/compositions/${encodeURIComponent(comp.id)}?section=Changes`,
  );
  render(<AppContent />);
  expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(comp.name);
  expect(screen.getByText("Revisions panel")).toBeTruthy();
  expect(screen.queryByRole("dialog")).toBeNull();
  window.history.replaceState({}, "", "/compositions/a%2Fb");
  expect(routeState().compositionId).toBe("a/b");
});

it("preserves list filters and density across detail navigation and browser history changes", async () => {
  const user = userEvent.setup();
  const comp = INITIAL_MOCK_COMPOSITIONS[0];
  render(<AppContent />);
  await user.type(
    screen.getByRole("textbox", { name: "Search previews" }),
    comp.name,
  );
  await user.click(screen.getByRole("button", { name: "Row layout" }));
  const listSearch = window.location.search;
  await user.click(screen.getByRole("button", { name: comp.name }));
  expect(window.location.pathname).toBe(`/compositions/${comp.id}`);
  await user.click(screen.getByRole("button", { name: "All previews" }));
  expect(window.location.search).toBe(listSearch);
  expect(
    (
      screen.getByRole("textbox", {
        name: "Search previews",
      }) as HTMLInputElement
    ).value,
  ).toBe(comp.name);
  expect(
    screen
      .getByRole("button", { name: "Row layout" })
      .getAttribute("aria-pressed"),
  ).toBe("true");
  window.history.replaceState({}, "", "/compositions?phase=failed&q=no-match");
  fireEvent.popState(window);
  expect(
    screen.getByRole("heading", { name: "No previews match these filters" }),
  ).toBeTruthy();
  expect(
    (
      screen.getByRole("textbox", {
        name: "Search previews",
      }) as HTMLInputElement
    ).value,
  ).toBe("no-match");
});

it("keeps bindings, revisions, diagnostics, activity, update, and destroy available from details", async () => {
  const user = userEvent.setup();
  const comp = INITIAL_MOCK_COMPOSITIONS[0];
  window.history.replaceState({}, "", `/compositions/${comp.id}`);
  render(<AppContent />);
  expect(screen.queryByText("Frontend bindings panel")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Manage" }));
  expect(screen.getByText("Frontend bindings panel")).toBeTruthy();
  for (const [section, panel] of [
    ["Changes", "Revisions panel"],
    ["Logs", "Diagnostics panel"],
    ["History", "Preview activity panel"],
  ]) {
    await user.click(
      within(
        screen.getByRole("navigation", { name: "Preview sections" }),
      ).getByRole("button", { name: section }),
    );
    expect(screen.getByText(panel)).toBeTruthy();
  }
  await user.click(screen.getByRole("button", { name: "Update preview" }));
  expect(
    screen.getByRole("dialog", { name: "Update preview form" }),
  ).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Destroy preview" }));
  const dialog = await screen.findByRole("dialog", {
    name: `Destroy ${comp.name}?`,
  });
  expect(destroyComposition).not.toHaveBeenCalled();
  await user.click(
    within(dialog).getByRole("button", { name: "Destroy preview" }),
  );
  expect(destroyComposition).toHaveBeenCalledWith(comp.id);
});

it("shows an unavailable deep link instead of reopening a stale selection", async () => {
  window.history.replaceState({}, "", "/compositions/missing");
  render(<AppContent />);
  expect(
    screen.getByRole("heading", { name: "Preview unavailable" }),
  ).toBeTruthy();
});

it.each([
  ["Diagnostics", "Diagnostics panel"],
  ["Activity", "Preview activity panel"],
])("preserves legacy %s section URLs", (legacy, panel) => {
  window.history.replaceState(
    {},
    "",
    `/compositions/${INITIAL_MOCK_COMPOSITIONS[0].id}?section=${legacy}`,
  );
  render(<AppContent />);
  expect(screen.getByText(panel)).toBeTruthy();
});
