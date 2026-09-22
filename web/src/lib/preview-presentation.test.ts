import { expect, it } from "vitest";
import { INITIAL_MOCK_COMPOSITIONS } from "./mock-data";
import {
  debugContext,
  lifecycleSummary,
  verificationSummary,
} from "./preview-presentation";
import type { Phase } from "../types/api";
it.each([
  "created",
  "provisioning",
  "updating",
  "ready",
  "completed",
  "suspended",
  "cancelled",
  "failed",
  "destroying",
  "destroyed",
] as Phase[])(
  "explains %s without equating observation and serving identity",
  (phase) => {
    const c = { ...INITIAL_MOCK_COMPOSITIONS[0], phase };
    expect(lifecycleSummary(c)).toBeTruthy();
    if (phase === "destroyed")
      expect(verificationSummary(c)).toBe("Endpoint withdrawn");
    const context = JSON.parse(debugContext(c));
    expect(context.note).toContain("not proof");
    expect(context.last_refreshed).toBe("Unavailable");
  },
);
it("excludes arbitrary fields and source credentials from copied context", () => {
  const c = { ...INITIAL_MOCK_COMPOSITIONS[0], env: { TOKEN: "do-not-copy" } };
  const out = debugContext(c, "2026-09-22T12:00:00Z");
  expect(out).not.toContain("do-not-copy");
  expect(out).toContain("2026-09-22T12:00:00Z");
  expect(out).toContain("Excluded");
});
