import { afterEach, expect, it, vi } from "vitest";
import { authConfig } from "./auth";

afterEach(() => vi.unstubAllGlobals());

it("reports an outdated server returning the SPA instead of login configuration", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response("<!doctype html><html></html>", {
        headers: { "Content-Type": "text/html" },
      }),
    ),
  );
  await expect(authConfig()).rejects.toThrow("Update the Envy server");
});

it.each(["{", "null", '{"mode":"unknown","csrf_token":"token"}', "{}"])(
  "rejects malformed login configuration: %s",
  async (body) => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(body, {
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    await expect(authConfig()).rejects.toThrow("unexpected response");
  },
);

it("loads valid password login configuration", async () => {
  const config = { mode: "password", csrf_token: "csrf" };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(config)));
  await expect(authConfig()).resolves.toEqual(config);
});
