import { afterEach, expect, it, vi } from "vitest";
import { ApiRequestError, EnvyApiClient } from "./api-client";
import type { CatalogManifest } from "../types/api";
vi.mock("./auth", () => ({ csrf: async () => "csrf-token" }));
afterEach(() => vi.unstubAllGlobals());
it("uses existing routes, encoded identities, CSRF, and exact discovery and approval bodies", async () => {
  const fetch = vi.fn().mockImplementation(async () => Response.json({}));
  vi.stubGlobal("fetch", fetch);
  const client = new EnvyApiClient();
  const manifest = { api_version: "envy/v1" } as CatalogManifest;
  await client.validateCatalog(manifest);
  await client.applyCatalog(manifest);
  const selection = {
    deployment: "pricing",
    env: { API_URL: "http://api.shop.svc.cluster.local" },
  };
  await client.discoverPreviewProfile(
    "shop space",
    "staging",
    "pricing",
    selection,
  );
  await client.inspectPreviewProfile("shop space", "staging", "pricing");
  const approval = {
    selection,
    inspection: "exact-fingerprint",
    expected_revision: 7,
    confirm_connectivity: true,
  };
  await client.approvePreviewProfile(
    "shop space",
    "staging",
    "pricing",
    approval,
  );
  expect(fetch.mock.calls.map((args) => args[0])).toEqual([
    "/v1/catalog/validate",
    "/v1/catalog/apply",
    "/v1/projects/shop%20space/baselines/staging/components/pricing/preview-profile/discover",
    "/v1/projects/shop%20space/baselines/staging/components/pricing/preview-profile",
    "/v1/projects/shop%20space/baselines/staging/components/pricing/preview-profile/approve",
  ]);
  expect(JSON.parse(fetch.mock.calls[2][1].body)).toEqual(selection);
  expect(JSON.parse(fetch.mock.calls[4][1].body)).toEqual(approval);
  expect(fetch.mock.calls[4][1].headers.get("X-CSRF-Token")).toBe("csrf-token");
  expect(fetch.mock.calls[4][1].credentials).toBe("same-origin");
});
it("preserves structured error status for distinguishing absent profiles from access failures", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json(
          { error: { code: "forbidden", message: "Access denied" } },
          { status: 403 },
        ),
      ),
  );
  await expect(
    new EnvyApiClient().inspectPreviewProfile("shop", "staging", "pricing"),
  ).rejects.toEqual(
    expect.objectContaining({ status: 403, code: "forbidden" }),
  );
  expect(new ApiRequestError("test", 404)).toBeInstanceOf(Error);
});
