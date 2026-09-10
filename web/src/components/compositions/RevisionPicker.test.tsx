import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { RevisionPicker, selectedOverride } from "./RevisionPicker";
import { apiClient } from "../../lib/api-client";
import {
  PublishedBuild,
  RevisionResolution,
  SourceRepository,
} from "../../types/api";
vi.mock("../../context/ApiContext", () => ({
  useEnvyApi: () => ({ isDemoMode: false, serverUrl: "", token: "" }),
}));
const repo: SourceRepository = {
  project: "demo",
  id: "backend",
  github_repository: "acme/backend",
  installation_id: 42,
  enabled: true,
  images: { "service-b": "registry.example.com/service-b" },
};
const sha = "a".repeat(40);
const build: PublishedBuild = {
  id: "b".repeat(64),
  project: "demo",
  repository: "backend",
  github_repository: "acme/backend",
  component: "service-b",
  revision: sha,
  image: `registry.example.com/service-b@sha256:${"c".repeat(64)}`,
  run_id: "123",
  attempt: 1,
  run_url: "https://github.com/acme/backend/actions/runs/123",
  built_at: "2026-09-10T10:00:00Z",
};
const resolution: RevisionResolution = {
  repository: repo,
  commit: { sha, message: "Historical change" },
  builds: [build],
  ci_url: "https://github.com/acme/backend/actions",
};
function Form() {
  const [value, setValue] = useState("");
  return (
    <>
      <RevisionPicker
        project="demo"
        component="service-b"
        profile="http-small"
        value={value}
        onChange={setValue}
      />
      <output data-testid="selection">
        {JSON.stringify(selectedOverride(value))}
      </output>
    </>
  );
}
beforeEach(() => {
  vi.spyOn(apiClient, "listSourceRepositories").mockResolvedValue([repo]);
  vi.spyOn(apiClient, "resolveRevision").mockResolvedValue(resolution);
});
afterEach(cleanup);
async function start() {
  const user = userEvent.setup();
  render(<Form />);
  await screen.findByText("acme/backend · Enabled");
  return user;
}
describe("published build selection", () => {
  it("freezes branch resolution, requires an explicit build, and refreshes the pinned SHA", async () => {
    const user = await start();
    await user.type(screen.getByLabelText("service-b Git revision"), "main");
    await user.click(screen.getByText("Resolve revision"));
    await screen.findByText(sha);
    expect(screen.getByTestId("selection").textContent).toBe('{"image":""}');
    await user.selectOptions(
      screen.getByLabelText("service-b published build"),
      build.id,
    );
    expect(screen.getByTestId("selection").textContent).toBe(
      JSON.stringify({ build_id: build.id }),
    );
    await user.click(screen.getByText("Refresh builds for this commit"));
    await waitFor(() =>
      expect(apiClient.resolveRevision).toHaveBeenLastCalledWith(
        "demo",
        "backend",
        "service-b",
        sha,
        "",
      ),
    );
    expect(screen.getByTestId("selection").textContent).toBe('{"image":""}');
  });
  it("shows missing builds without substituting an older image", async () => {
    vi.mocked(apiClient.resolveRevision).mockResolvedValue({
      ...resolution,
      builds: [],
    });
    const user = await start();
    await user.type(screen.getByLabelText("service-b Git revision"), "main");
    await user.click(screen.getByText("Resolve revision"));
    await screen.findByText(/Commit exists; no published build available/);
    expect(
      screen.getByRole("link", { name: "Open CI" }).getAttribute("href"),
    ).toBe(resolution.ci_url);
    expect(screen.queryByLabelText("service-b published build")).toBeNull();
  });
  it("validates full SHAs before lookup and accepts historical commits", async () => {
    const user = await start();
    await user.selectOptions(
      screen.getByLabelText("service-b revision type"),
      "sha",
    );
    await user.type(screen.getByLabelText("service-b Git revision"), "abc");
    await user.click(screen.getByText("Resolve revision"));
    await screen.findByText(/40-character/);
    expect(apiClient.resolveRevision).not.toHaveBeenCalled();
    await user.clear(screen.getByLabelText("service-b Git revision"));
    await user.type(screen.getByLabelText("service-b Git revision"), sha);
    await user.click(screen.getByText("Resolve revision"));
    await screen.findByText("Historical change");
    expect(apiClient.resolveRevision).toHaveBeenCalledWith(
      "demo",
      "backend",
      "service-b",
      sha,
      "",
    );
  });
  it("discards a stale response after editing the revision", async () => {
    let finish!: (value: RevisionResolution) => void;
    vi.mocked(apiClient.resolveRevision).mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const user = await start();
    await user.type(screen.getByLabelText("service-b Git revision"), "main");
    await user.click(screen.getByText("Resolve revision"));
    await user.clear(screen.getByLabelText("service-b Git revision"));
    await user.type(
      screen.getByLabelText("service-b Git revision"),
      "feature/new",
    );
    finish(resolution);
    await waitFor(() =>
      expect(screen.queryByText("Resolved commit (pinned)")).toBeNull(),
    );
    expect(screen.getByTestId("selection").textContent).toBe('{"image":""}');
  });
  it("browses branches and historical commits", async () => {
    vi.spyOn(apiClient, "sourceBranches").mockResolvedValue({
      items: [{ name: "feature/payments", sha }],
      page: 1,
      has_more: false,
    });
    vi.spyOn(apiClient, "sourceCommits").mockResolvedValue({
      items: [{ sha, message: "Last week" }],
      page: 1,
      has_more: false,
    });
    const user = await start();
    await user.click(screen.getByText("Browse branches"));
    await user.selectOptions(
      await screen.findByLabelText("service-b branch"),
      "feature/payments",
    );
    await user.click(screen.getByText("Browse commits"));
    await user.selectOptions(
      await screen.findByLabelText("service-b historical commit"),
      sha,
    );
    await screen.findByText("Historical change");
    expect(apiClient.resolveRevision).toHaveBeenCalledWith(
      "demo",
      "backend",
      "service-b",
      sha,
      "",
    );
  });
  it("shows disabled repositories and prevents lookups", async () => {
    vi.mocked(apiClient.listSourceRepositories).mockResolvedValue([
      { ...repo, enabled: false },
    ]);
    render(<Form />);
    await screen.findByText("This repository is disabled.");
    expect(screen.queryByText("Resolve revision")).toBeNull();
  });
});
