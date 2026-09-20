import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import { ApiProvider, useEnvyApi } from "./ApiContext";
import { INITIAL_MOCK_COMPOSITIONS } from "../lib/mock-data";
import { apiClient } from "../lib/api-client";
import type { Composition } from "../types/api";
afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.restoreAllMocks();
});
it("does not insert an old live creation into demo state after switching connections", async () => {
  vi.spyOn(apiClient, "session").mockResolvedValue({
    principal: { kind: "human", id: "alice", display_name: "Alice" },
    auth_mode: "dev",
    channel: "web",
    capabilities: [],
  });
  vi.spyOn(apiClient, "installation").mockResolvedValue({
    id: "one",
    version: "dev",
    auth_mode: "dev",
    default_ttl: "1h",
    max_ttl: "4h",
    max_compositions: 20,
  });
  vi.spyOn(apiClient, "listProjects").mockResolvedValue([]);
  vi.spyOn(apiClient, "listCompositions").mockResolvedValue([]);
  let resolve!: (composition: Composition) => void;
  vi.spyOn(apiClient, "createComposition").mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  let ctx!: ReturnType<typeof useEnvyApi>;
  function Harness() {
    ctx = useEnvyApi();
    return (
      <div>
        {ctx.serverStatus} {ctx.compositions.map((c) => c.id).join(",")}
      </div>
    );
  }
  render(
    <ApiProvider>
      <Harness />
    </ApiProvider>,
  );
  await screen.findByText("connected");
  let creation!: Promise<Composition>;
  act(() => {
    creation = ctx.createComposition({
      project: "shop",
      baseline: "staging",
      name: "old-connection",
      overrides: {},
    });
  });
  act(() => {
    ctx.setDemoMode(true);
  });
  await act(async () => {
    resolve({ id: "late-live-preview" } as Composition);
    await creation;
  });
  expect(screen.queryByText(/late-live-preview/)).toBeNull();
  expect(ctx.isDemoMode).toBe(true);
});

it("restores the saved demo workspace on reload without querying the live API", () => {
  localStorage.setItem("envy_demo_mode", "true");
  const fetch = vi.spyOn(globalThis, "fetch");
  function Probe() {
    const { compositions, projects, baselines, components } = useEnvyApi();
    return (
      <p>
        {compositions.length} previews / {projects.length} projects /{" "}
        {baselines.length} baselines / {components.length} components
      </p>
    );
  }
  render(
    <ApiProvider>
      <Probe />
    </ApiProvider>,
  );
  expect(
    screen.getByText(
      `${INITIAL_MOCK_COMPOSITIONS.length} previews / 1 projects / 1 baselines / 3 components`,
    ),
  ).toBeTruthy();
  expect(fetch).not.toHaveBeenCalled();
});
