import { useState } from "react";
import { useEnvyApi } from "../../context/ApiContext";
import { apiClient } from "../../lib/api-client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import type { Baseline, Component, Project } from "../../types/api";

type Kind = "project" | "component" | "baseline";
function example(kind: Kind, project: string) {
  if (kind === "project") return { id: "orders", name: "Orders" };
  if (kind === "component")
    return {
      id: "service-b",
      project,
      protocol: "http",
      port: 8080,
      health_path: "/healthz",
      readiness_path: "/readyz",
      profile: "http-small",
      overridable: true,
    };
  return {
    id: "staging",
    project,
    revision: "v1",
    endpoint: `http://${project}.envy.localhost:8080`,
    routing: {
      namespace: `${project}-baseline`,
      gateway: "envy-preview",
      entry_component: "gateway",
    },
    verification: {
      kind: "envy-chain",
      chain: ["gateway", "service-a", "service-b"],
    },
    components: Object.fromEntries(
      ["gateway", "service-a", "service-b"].map((id) => [
        id,
        {
          service_host: `${id}.${project}-baseline.svc.cluster.local`,
          port: 8080,
          image: `envy/${id}:v1`,
        },
      ]),
    ),
  };
}
export function CatalogRegistration() {
  const { isDemoMode, projects, refreshAll } = useEnvyApi();
  const [kind, setKind] = useState<Kind>("project");
  const [project, setProject] = useState(projects[0]?.id || "orders");
  const [body, setBody] = useState(
    JSON.stringify(example("project", project), null, 2),
  );
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [expanded, setExpanded] = useState(false);
  const reset = (next: Kind, scope: string) => {
    setKind(next);
    setProject(scope);
    setBody(JSON.stringify(example(next, scope), null, 2));
    setMessage("");
  };
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setMessage("");
    try {
      const value = JSON.parse(body);
      if (kind === "project") await apiClient.registerProject(value as Project);
      else if (kind === "component")
        await apiClient.registerComponent({ ...value, project } as Component);
      else await apiClient.registerBaseline({ ...value, project } as Baseline);
      setMessage(`Registered ${kind} ${value.id}.`);
      await refreshAll();
    } catch (error) {
      setMessage(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="rounded-xl border border-border bg-card/60 p-4 space-y-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h3 className="font-semibold">Register catalog entries</h3>
          <p className="text-xs text-muted-foreground">
            Register a project, its approved profiles, then existing baseline
            bindings. Entries are immutable.
          </p>
        </div>
        <Button variant="outline" onClick={() => setExpanded(!expanded)}>
          {expanded ? "Close registration" : "Register"}
        </Button>
      </div>
      {expanded &&
        (isDemoMode ? (
          <p className="text-sm text-muted-foreground">
            Switch to a live API connection to register catalog entries.
          </p>
        ) : (
          <form onSubmit={submit} className="space-y-3">
            <div className="flex gap-3">
              <label className="text-xs">
                Entry type
                <select
                  aria-label="Entry type"
                  value={kind}
                  disabled={busy}
                  onChange={(e) => reset(e.target.value as Kind, project)}
                  className="block rounded border border-input bg-card p-2"
                >
                  <option value="project">Project</option>
                  <option value="component">Component</option>
                  <option value="baseline">Baseline</option>
                </select>
              </label>
              {kind !== "project" && (
                <label className="text-xs">
                  Project ID
                  <Input
                    aria-label="Registration project"
                    value={project}
                    disabled={busy}
                    onChange={(e) => reset(kind, e.target.value)}
                    required
                  />
                </label>
              )}
            </div>
            <label className="block text-xs space-y-1">
              <span>Registration JSON</span>
              <textarea
                aria-label="Registration JSON"
                value={body}
                onChange={(e) => setBody(e.target.value)}
                disabled={busy}
                rows={14}
                className="w-full rounded border border-input bg-background p-3 font-mono text-xs"
                required
              />
            </label>
            <p className="text-xs text-muted-foreground">
              Baseline registration checks live sidecar connectivity and routing
              ownership. Environment values are visible in the catalog; use no
              credentials.
            </p>
            <Button type="submit" disabled={busy}>
              {busy ? "Validating…" : `Register ${kind}`}
            </Button>
            {message && (
              <p role="status" className="text-sm whitespace-pre-wrap">
                {message}
              </p>
            )}
          </form>
        ))}
    </section>
  );
}
