import { useEffect, useState } from "react";
import { History, RefreshCw } from "lucide-react";
import { apiClient } from "../../lib/api-client";
import { Activity } from "../../types/api";
import { useEnvyApi } from "../../context/ApiContext";
import { Button } from "../ui/button";
import { Input } from "../ui/input";

export function ActivityView() {
  const { isDemoMode, projects, installation } = useEnvyApi();
  const [items, setItems] = useState<Activity[]>([]);
  const [next, setNext] = useState("");
  const [actor, setActor] = useState("");
  const [project, setProject] = useState("");
  const [action, setAction] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  async function load(after = "") {
    if (isDemoMode) return;
    setLoading(true);
    setError("");
    try {
      const page = await apiClient.listActivity(
        Object.fromEntries(
          Object.entries({ actor, project, action, after }).filter(
            ([, v]) => v,
          ),
        ),
      );
      setItems((old) => (after ? [...old, ...page.items] : page.items));
      setNext(page.next_cursor || "");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to load activity");
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void load();
  }, [isDemoMode]);
  return (
    <div className="space-y-5">
      <p className="text-sm text-muted-foreground">
        Operational history retention:{" "}
        {installation?.audit_retention || "retained"}. Records before an
        operator-configured retention boundary are unavailable.
      </p>
      <div className="flex flex-col gap-3 rounded-xl border border-border bg-card p-4 sm:flex-row sm:items-end">
        <label className="flex-1 text-sm font-medium">
          Actor
          <Input
            value={actor}
            onChange={(e) => setActor(e.target.value)}
            placeholder="Identity ID"
            className="mt-1"
          />
        </label>
        <label className="flex-1 text-sm font-medium">
          Project
          <select
            value={project}
            onChange={(e) => setProject(e.target.value)}
            className="mt-1 h-9 w-full rounded-md border border-input bg-background px-3"
          >
            <option value="">All projects</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <label className="flex-1 text-sm font-medium">
          Action
          <Input
            value={action}
            onChange={(e) => setAction(e.target.value)}
            placeholder="composition.update"
            className="mt-1"
          />
        </label>
        <Button onClick={() => void load()} disabled={loading}>
          <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
          Apply filters
        </Button>
      </div>
      {isDemoMode ? (
        <p className="rounded-xl border border-border bg-card p-6 text-muted-foreground">
          Operational history is available in Live Mode.
        </p>
      ) : error ? (
        <p
          role="alert"
          className="rounded-xl border border-rose-300 bg-rose-50 p-4 text-rose-800"
        >
          {error}
        </p>
      ) : items.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border p-10 text-center text-muted-foreground">
          No matching activity.
        </p>
      ) : (
        <>
          <ol className="space-y-3">
            {items.map((item) => (
              <li
                key={item.id}
                className="rounded-xl border border-border bg-card p-4"
              >
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div className="flex items-center gap-2">
                    <History className="h-4 w-4" />
                    <strong>{item.action}</strong>
                    <span className="rounded bg-muted px-2 py-0.5 text-xs">
                      {item.outcome}
                    </span>
                  </div>
                  <time className="text-xs text-muted-foreground">
                    {new Date(item.occurred_at).toLocaleString()}
                  </time>
                </div>
                <p className="mt-2 text-sm">
                  <span className="font-medium">
                    {item.actor.display_name || item.actor.id}
                  </span>{" "}
                  <span className="text-muted-foreground">
                    via {item.channel}
                  </span>
                </p>
                <p className="mt-1 font-mono text-xs text-muted-foreground">
                  {item.resource_type}: {item.resource_id}
                  {item.generation_to
                    ? ` · generation ${item.generation_from || 0} → ${item.generation_to}`
                    : ""}
                </p>
                {item.task && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    Task: {item.task}
                  </p>
                )}
              </li>
            ))}
          </ol>
          {next && (
            <Button
              variant="outline"
              disabled={loading}
              onClick={() => void load(next)}
            >
              Load older activity
            </Button>
          )}
        </>
      )}
    </div>
  );
}
