import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { INITIAL_MOCK_COMPOSITIONS } from "../../lib/mock-data";
import { CompositionDetailView } from "./CompositionDetailView";

vi.mock("./FrontendBindings", () => ({ FrontendBindings: () => null }));
vi.mock("./CompositionDiagnostics", () => ({
  CompositionDiagnostics: () => null,
}));
vi.mock("./CompositionRevisions", () => ({ CompositionRevisions: () => null }));
vi.mock("./CompositionActivity", () => ({ CompositionActivity: () => null }));
vi.mock("../../context/ApiContext", () => ({
  useEnvyApi: () => ({
    baselines: [
      {
        project: "demo",
        id: "staging",
        endpoint: "http://shop.envy.localhost:8080",
        routing: { preview_selector: { header: "X-Envy-Preview" } },
      },
    ],
  }),
}));
afterEach(cleanup);

it("shows capture metadata and inspection without automatic acknowledgement", () => {
  const name = "projects/test-project/subscriptions/preview";
  render(
    <CompositionDetailView
      composition={{
        ...INITIAL_MOCK_COMPOSITIONS[0],
        message_isolation: true,
        message_subscriptions: [
          {
            name,
            topic: "projects/test-project/topics/orders",
            component: "billing",
            subscription_env: "SUB",
            filter: 'attributes.envy_composition = "preview"',
            retention: "604800s",
            expires_at: "2026-09-16T00:00:00Z",
            ready: true,
            backlog_may_be_lost: true,
          },
        ],
      }}
      onBack={vi.fn()}
      onUpdate={vi.fn()}
      onDestroy={vi.fn()}
      section="Overview"
      onSectionChange={vi.fn()}
    />,
  );
  expect(
    screen.getByRole("heading", { name: "Message isolation: Enabled" }),
  ).toBeTruthy();
  expect(screen.getByText(name)).toBeTruthy();
  const command = screen.getByText(
    /^gcloud pubsub subscriptions pull/,
  ).textContent!;
  expect(command).toContain("--limit=10 --format=json");
  expect(command).not.toContain("--auto-ack");
  expect(screen.getByRole("alert").textContent).toContain(
    "previously queued messages may be lost",
  );
});

it("offers a same-endpoint selector command when the baseline enables it", () => {
  render(
    <CompositionDetailView
      composition={INITIAL_MOCK_COMPOSITIONS[0]}
      onBack={vi.fn()}
      onUpdate={vi.fn()}
      onDestroy={vi.fn()}
      section="Overview"
      onSectionChange={vi.fn()}
    />,
  );

  expect(
    screen.getByRole("button", {
      name: "Copy baseline selector curl command",
    }),
  ).toBeTruthy();
  expect(screen.getByText("X-Envy-Preview")).toBeTruthy();
});
