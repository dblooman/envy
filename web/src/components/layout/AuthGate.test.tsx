import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  act,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AuthGate } from "./AuthGate";
import { authConfig, authPost } from "../../lib/auth";
vi.mock("../../lib/auth", () => ({
  authConfig: vi.fn(),
  authPost: vi.fn(),
  returnTo: () => "/compositions/abc",
}));
beforeEach(() => {
  localStorage.clear();
  window.history.replaceState({}, "", "/compositions/abc");
  vi.mocked(authConfig).mockResolvedValue({
    mode: "password",
    csrf_token: "csrf",
  });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
it("shows only password login and does not mount application data before authentication", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 401 }));
  render(
    <AuthGate>
      <p>Application data</p>
    </AuthGate>,
  );
  expect(await screen.findByLabelText("Password")).toBeTruthy();
  expect(screen.queryByText("Application data")).toBeNull();
  expect(screen.queryByText("Continue with Google")).toBeNull();
  expect(screen.queryByText("Connection settings")).toBeNull();
});
it("shows a failed password response without mounting application data", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 401 }));
  vi.mocked(authPost).mockRejectedValue(
    new Error("Invalid username or password."),
  );
  render(
    <AuthGate>
      <p>Application data</p>
    </AuthGate>,
  );
  fireEvent.change(await screen.findByLabelText("Password"), {
    target: { value: "wrong" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    "Invalid username or password.",
  );
  expect(authPost).toHaveBeenCalledWith("/auth/password", {
    username: "admin",
    password: "wrong",
  });
  expect(screen.queryByText("Application data")).toBeNull();
});
it("shows only Google login and a clear denied-access message", async () => {
  window.history.replaceState({}, "", "/login?error=access_denied");
  vi.mocked(authConfig).mockResolvedValue({
    mode: "google",
    csrf_token: "csrf",
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 401 }));
  render(
    <AuthGate>
      <p>Application data</p>
    </AuthGate>,
  );
  expect(
    await screen.findByRole("button", { name: "Continue with Google" }),
  ).toBeTruthy();
  expect(screen.getByRole("alert").textContent).toContain("not allowed access");
  expect(screen.queryByLabelText("Password")).toBeNull();
});
it("opens an authenticated workspace and unmounts its data when the session expires", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200 }));
  render(
    <AuthGate>
      <p>Application data</p>
    </AuthGate>,
  );
  expect(await screen.findByText("Application data")).toBeTruthy();
  act(() => window.dispatchEvent(new Event("envy-unauthorized")));
  expect(screen.queryByText("Application data")).toBeNull();
  expect(screen.getByRole("alert").textContent).toContain("expired");
});
it("keeps demo mode independent of live authentication", () => {
  localStorage.setItem("envy_demo_mode", "true");
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  render(
    <AuthGate>
      <p>Demo data</p>
    </AuthGate>,
  );
  expect(screen.getByText("Demo data")).toBeTruthy();
  expect(fetch).not.toHaveBeenCalled();
});
it("fails closed when session discovery is unavailable", async () => {
  vi.mocked(authConfig).mockRejectedValue(
    new Error("Unable to load login settings."),
  );
  render(
    <AuthGate>
      <p>Application data</p>
    </AuthGate>,
  );
  await waitFor(() =>
    expect(screen.getByRole("alert").textContent).toContain("Unable to load"),
  );
  expect(screen.queryByText("Application data")).toBeNull();
});
