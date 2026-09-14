import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ActivityView } from "./ActivityView";
import { apiClient } from "../../lib/api-client";

vi.mock("../../context/ApiContext", () => ({
  useEnvyApi: () => ({ isDemoMode: false, projects: [], installation: null }),
}));
afterEach(cleanup);
it("applies filters explicitly and keeps them stable while paging", async () => {
  const list = vi.spyOn(apiClient, "listActivity").mockResolvedValue({
    items: [
      {
        id: "event-1",
        occurred_at: "2026-09-14T12:00:00Z",
        actor: { id: "alice", kind: "human" },
        channel: "web",
        action: "composition.create",
        outcome: "success",
        resource_type: "composition",
        resource_id: "cmp-1",
      },
    ],
    next_cursor: "older",
  });
  render(<ActivityView />);
  await waitFor(() => expect(list).toHaveBeenCalledTimes(1));
  fireEvent.change(screen.getByLabelText("Actor"), {
    target: { value: "alice" },
  });
  expect(list).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
  await waitFor(() =>
    expect(list).toHaveBeenLastCalledWith({ actor: "alice" }),
  );
  await waitFor(() =>
    expect(
      screen
        .getByRole("button", { name: "Load older activity" })
        .hasAttribute("disabled"),
    ).toBe(false),
  );
  fireEvent.change(screen.getByLabelText("Actor"), {
    target: { value: "unapplied" },
  });
  list.mockResolvedValueOnce({ items: [], next_cursor: "" });
  fireEvent.click(screen.getByRole("button", { name: "Load older activity" }));
  await waitFor(() =>
    expect(list).toHaveBeenLastCalledWith({ actor: "alice", after: "older" }),
  );
});
