import type { Composition } from "../types/api";
export function lifecycleSummary(c: Composition): string {
  const labels: Record<string, string> = {
    created: "Preview requested",
    provisioning: "Starting preview services",
    updating: "Applying requested changes",
    ready: "Ready to test",
    completed: "Execution completed",
    suspended: "Scheduled execution suspended",
    cancelled: "Execution cancelled",
    failed: "Preview needs attention",
    destroying: "Removing preview resources",
    destroyed: "Preview removed; deployment history is still available",
  };
  return labels[c.phase] || c.phase;
}
export function verificationSummary(c: Composition): string {
  if (c.phase === "destroyed") return "Endpoint withdrawn";
  if (c.phase === "destroying") return "Endpoint withdrawal in progress";
  if (["completed", "suspended", "cancelled"].includes(c.phase))
    return "Execution status is separate from HTTP verification";
  if (!c.endpoints.public.ready) return "Endpoint not ready for use";
  return c.verification_level === "routing"
    ? "Request routing verified"
    : c.verification_level === "reachability"
      ? "HTTP reachable · routing unverified"
      : "Endpoint ready · routing unverified";
}
export function debugContext(c: Composition, refreshed?: string): string {
  return JSON.stringify(
    {
      preview: c.id,
      name: c.name,
      project: c.project,
      baseline: c.baseline,
      baseline_revision: c.baseline_revision,
      requested_generation: c.generation,
      observed_generation: c.observed_generation,
      note: "Observed generation is not proof of the version serving a request.",
      phase: c.phase,
      last_refreshed: refreshed || "Unavailable",
      expires_at: c.expires_at,
      requested: Object.fromEntries(
        Object.entries(c.overrides).map(([name, o]) => [
          name,
          {
            image: o.image,
            build_id: o.build_id,
            commit: o.source?.revision || "Unavailable",
            ci: o.source?.run_url,
          },
        ]),
      ),
      components: c.components,
      verification_level: c.verification_level || "none",
      conditions: c.conditions,
      error: c.last_error || c.latest_operation.error || null,
      endpoint: c.endpoints.public,
      logs: "Excluded; request a scoped snapshot separately",
    },
    null,
    2,
  );
}
