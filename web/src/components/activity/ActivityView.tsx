import { useEffect, useState, useCallback } from "react";
import { History, RefreshCw } from "lucide-react";
import { apiClient } from "../../lib/api-client";
import { Activity } from "../../types/api";
import { useEnvyApi } from "../../context/ApiContext";
import { Button } from "../ui/button";
import {
  activityActions,
  activityLabel,
} from "../../lib/activity-presentation";
import { Badge } from "../ui/badge";
import { Input } from "../ui/input";

export function ActivityView() {
  const { isDemoMode, projects, installation } = useEnvyApi();
  const [items, setItems] = useState<Activity[]>([]);
  const [next, setNext] = useState("");
  const [actor, setActor] = useState("");
  const [resource, setResource] = useState("");
  const [project, setProject] = useState("");
  const [action, setAction] = useState("");
  const [filters, setFilters] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const load = useCallback(
    async (after = "") => {
      if (isDemoMode) return;
      setLoading(true);
      setError("");
      try {
        const page = await apiClient.listActivity(
          Object.fromEntries(
            Object.entries({ ...filters, after }).filter(([, v]) => v),
          ),
        );
        setItems((old) => (after ? [...old, ...page.items] : page.items));
        setNext(page.next_cursor || "");
      } catch (e) {
        setError(e instanceof Error ? e.message : "Unable to load activity");
      } finally {
        setLoading(false);
      }
    },
    [isDemoMode, filters],
  );
  useEffect(() => {
    void load();
  }, [load]);
  return (
    <div className="space-y-5">
      <p className="text-sm text-muted-foreground">
        Previews are called compositions in the API and CLI. Operational history
        retention: {installation?.audit_retention || "retained"}. Records before
        an operator-configured retention boundary are unavailable.
      </p>
      <div className="flex flex-col gap-3 rounded-xl border border-border bg-card p-4 lg:flex-row lg:items-end">
        <label className="flex-1 text-sm font-medium">
          Actor
          <Input
            value={actor}
            onChange={(e) => setActor(e.target.value)}
            placeholder="e.g. local:admin"
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
            placeholder="Choose or enter an action"
            list="activity-actions"
            className="mt-1"
          />
        </label>
        <datalist id="activity-actions">
          {Object.entries(activityActions).map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </datalist>
        <label className="flex-1 text-sm font-medium">
          Resource ID
          <Input
            value={resource}
            onChange={(e) => setResource(e.target.value)}
            placeholder="Preview or operation resource ID"
            className="mt-1"
          />
        </label>
        <Button
          onClick={() =>
            setFilters({ actor, project, action, resource_id: resource })
          }
          disabled={loading}
        >
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
      ) : loading && !items.length ? (
        <p role="status">Loading activity…</p>
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
                    <strong>{activityLabel(item.action)}</strong>
                    <Badge
                      variant={
                        item.outcome === "succeeded"
                          ? "success"
                          : ["failed", "rejected"].includes(item.outcome)
                            ? "destructive"
                            : "secondary"
                      }
                    >
                      {item.outcome}
                    </Badge>
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
                <p className="mt-1 font-mono text-xs text-muted-foreground wrap-anywhere">
                  {item.composition || item.resource_type === "composition" ? (
                    <a
                      className="underline"
                      href={`/compositions/${encodeURIComponent(item.composition || item.resource_id)}?section=History`}
                    >
                      {item.resource_type === "composition"
                        ? "Preview"
                        : item.resource_type}
                      : {item.resource_id}
                    </a>
                  ) : (
                    <>
                      {item.resource_type === "composition"
                        ? "Preview"
                        : item.resource_type}
                      : {item.resource_id}
                    </>
                  )}
                  {item.generation_to
                    ? ` · revision ${item.generation_from ? `${item.generation_from} → ` : ""}${item.generation_to}`
                    : ""}
                </p>
                {item.operation && (
                  <p className="text-xs">Operation: {item.operation}</p>
                )}
                <details className="mt-2 text-xs">
                  <summary>Technical details</summary>
                  <pre className="envy-code">
                    {JSON.stringify(item, null, 2)}
                  </pre>
                </details>
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
