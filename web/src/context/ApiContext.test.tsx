import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ApiProvider, useEnvyApi } from "./ApiContext";
import { INITIAL_MOCK_COMPOSITIONS } from "../lib/mock-data";

afterEach(() => {
  cleanup();
  localStorage.clear();
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
