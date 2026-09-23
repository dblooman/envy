import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { VerificationEvidence } from "../../types/api";
import { afterEach, expect, it, vi } from "vitest";
import { PreviewEvidence, ExternalObservability } from "./PreviewEvidence";
import { apiClient, ApiRequestError } from "../../lib/api-client";
import { INITIAL_MOCK_COMPOSITIONS } from "../../lib/mock-data";
vi.mock("../../context/ApiContext", () => ({
  useEnvyApi: () => ({ isDemoMode: false }),
}));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
it("shows an explicit unsupported state for an older server", async () => {
  vi.spyOn(apiClient, "verification").mockRejectedValue(
    new ApiRequestError("not found", 404),
  );
  render(<PreviewEvidence composition={INITIAL_MOCK_COMPOSITIONS[0]} />);
  expect(
    await screen.findByText(
      "Verification evidence is not supported by this server.",
    ),
  ).toBeTruthy();
});
it("keeps older checks historical and HTTP evidence free of invented hops", async () => {
  const c = { ...INITIAL_MOCK_COMPOSITIONS[0], generation: 3 };
  vi.spyOn(apiClient, "verification").mockResolvedValue({
    items: [
      {
        id: "1",
        composition: c.id,
        generation: 2,
        kind: "http",
        outcome: "passed",
        first_checked_at: "2026-09-22T10:00:00Z",
        last_checked_at: "2026-09-22T10:01:00Z",
        probes: [
          { target: "preview", expected_status: 200, observed_status: 200 },
        ],
        hops: [],
      },
    ],
  });
  render(<PreviewEvidence composition={c} />);
  expect(await screen.findByText("Historical")).toBeTruthy();
  expect(screen.queryByText("Observed request path")).toBeNull();
});
it("shows empty configuration without blocking previews", async () => {
  vi.spyOn(apiClient, "observability").mockResolvedValue({ items: [] });
  render(<ExternalObservability id="preview" />);
  expect(
    await screen.findByText(/No links configured for this scope/),
  ).toBeTruthy();
});

const check: VerificationEvidence = {
  id: "new",
  composition: "test",
  generation: 3,
  kind: "envy-chain",
  outcome: "failed",
  first_checked_at: "2026-09-22T10:00:00Z",
  last_checked_at: "2026-09-22T10:01:00Z",
  probes: [{ target: "preview", expected_status: 200, observed_status: 503 }],
  hops: [
    {
      service: "api",
      version: "v3",
      composition: "test",
      deployment_composition: "test",
      workload_id: "workload-123",
    },
  ],
  error: {
    code: "verification_failed",
    message: "Wrong service answered",
    retryable: false,
  },
};
it("shows current failure without promoting an earlier pass and opens technical evidence", async () => {
  vi.spyOn(apiClient, "verification").mockResolvedValue({
    items: [check, { ...check, id: "old", outcome: "passed" }],
  });
  const user = userEvent.setup();
  render(
    <PreviewEvidence
      composition={{
        ...INITIAL_MOCK_COMPOSITIONS[0],
        generation: 3,
        phase: "ready",
      }}
    />,
  );
  expect(await screen.findByText("Wrong service answered")).toBeTruthy();
  expect(screen.getByText(/incomplete or failed verification/)).toBeTruthy();
  expect(screen.getByText("Historical · earlier check")).toBeTruthy();
  expect(screen.queryByText("workload-123")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Technical evidence" }));
  expect(screen.getByRole("dialog")).toBeTruthy();
  expect(screen.getByText("workload-123")).toBeTruthy();
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(document.activeElement).toBe(
    screen.getByRole("button", { name: "Technical evidence" }),
  );
});
it.each([
  ["stale", "Stale evidence · baseline or revision changed"],
  ["unavailable", "Baseline observation unavailable · proof is unknown"],
  [
    "unknown_coverage",
    "Fingerprint coverage unknown · check is not current proof",
  ],
] as const)(
  "does not present %s proof as current",
  async (freshness, label) => {
    vi.spyOn(apiClient, "verification").mockResolvedValue({
      items: [{ ...check, outcome: "passed", freshness }],
    });
    render(
      <PreviewEvidence
        composition={{ ...INITIAL_MOCK_COMPOSITIONS[0], generation: 3 }}
      />,
    );
    expect(await screen.findByText(label)).toBeTruthy();
    expect(screen.queryByText("Current evidence")).toBeNull();
  },
);
it("keeps removed preview evidence historical", async () => {
  vi.spyOn(apiClient, "verification").mockResolvedValue({
    items: [{ ...check, outcome: "passed" }],
  });
  render(
    <PreviewEvidence
      composition={{
        ...INITIAL_MOCK_COMPOSITIONS[0],
        generation: 3,
        phase: "destroyed",
      }}
    />,
  );
  expect(await screen.findByText("Historical")).toBeTruthy();
  expect(screen.queryByText("Current revision")).toBeNull();
});
it("retains evidence on refresh failure and clears it when changing previews", async () => {
  const request = vi
    .spyOn(apiClient, "verification")
    .mockResolvedValueOnce({ items: [check] })
    .mockRejectedValue(new Error("offline"));
  const user = userEvent.setup();
  const { rerender } = render(
    <PreviewEvidence
      composition={{
        ...INITIAL_MOCK_COMPOSITIONS[0],
        generation: 3,
        phase: "ready",
      }}
    />,
  );
  await screen.findByText("Wrong service answered");
  await user.click(screen.getByRole("button", { name: "Refresh evidence" }));
  expect(await screen.findByText(/Displayed checks may be stale/)).toBeTruthy();
  expect(screen.getByText("Wrong service answered")).toBeTruthy();
  request.mockResolvedValue({ items: [] });
  rerender(
    <PreviewEvidence
      composition={{
        ...INITIAL_MOCK_COMPOSITIONS[0],
        id: "different",
        generation: 3,
        phase: "ready",
      }}
    />,
  );
  expect(
    await screen.findByText("No verification recorded for this revision"),
  ).toBeTruthy();
  expect(screen.queryByText("Wrong service answered")).toBeNull();
});
it("loads older evidence with the returned cursor", async () => {
  const request = vi
    .spyOn(apiClient, "verification")
    .mockResolvedValueOnce({ items: [check], next_cursor: "cursor" })
    .mockResolvedValueOnce({
      items: [{ ...check, id: "older", generation: 2 }],
    });
  render(
    <PreviewEvidence
      composition={{
        ...INITIAL_MOCK_COMPOSITIONS[0],
        generation: 3,
        phase: "ready",
      }}
    />,
  );
  await userEvent.click(
    await screen.findByRole("button", { name: "Load older checks" }),
  );
  expect(
    await screen.findByRole("button", { name: "View check older, revision 2" }),
  ).toBeTruthy();
  expect(request).toHaveBeenLastCalledWith(
    INITIAL_MOCK_COMPOSITIONS[0].id,
    "cursor",
  );
});
