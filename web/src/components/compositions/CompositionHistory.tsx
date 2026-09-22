import { useEffect, useState } from "react";
import { apiClient } from "../../lib/api-client";
import { useEnvyApi } from "../../context/ApiContext";
import type { Composition, Activity, LifecycleEvent } from "../../types/api";
import { activityLabel } from "../../lib/activity-presentation";
import { Button } from "../ui/button";
export function CompositionHistory({
  composition,
}: {
  composition: Composition;
}) {
  const { isDemoMode } = useEnvyApi();
  const [events, setEvents] = useState<LifecycleEvent[]>([]);
  const [activity, setActivity] = useState<Activity[]>([]);
  const [eventCursor, setEventCursor] = useState("");
  const [activityCursor, setActivityCursor] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    const c = new AbortController();
    setEvents([]);
    setActivity([]);
    setError("");
    setEventCursor("");
    setActivityCursor("");
    if (!isDemoMode) {
      setLoading(true);
      Promise.allSettled([
        apiClient.listCompositionEvents(composition.id, "", c.signal),
        apiClient.listActivity({ resource_id: composition.id }, c.signal),
      ])
        .then(([e, a]) => {
          if (c.signal.aborted) return;
          if (e.status === "fulfilled") {
            setEvents(e.value.items);
            setEventCursor(e.value.next_cursor || "");
          }
          if (a.status === "fulfilled") {
            setActivity(a.value.items);
            setActivityCursor(a.value.next_cursor || "");
          }
          if (e.status === "rejected" || a.status === "rejected")
            setError(
              "Some history could not be loaded. Available records remain visible.",
            );
        })
        .finally(() => {
          if (!c.signal.aborted) setLoading(false);
        });
    }
    return () => c.abort();
  }, [composition.id, composition.generation, isDemoMode]);
  async function more(kind: "events" | "activity") {
    setLoading(true);
    try {
      if (kind === "events") {
        const p = await apiClient.listCompositionEvents(
          composition.id,
          eventCursor,
        );
        setEvents((old) => [...old, ...p.items]);
        setEventCursor(p.next_cursor || "");
      } else {
        const p = await apiClient.listActivity({
          resource_id: composition.id,
          after: activityCursor,
        });
        setActivity((old) => [...old, ...p.items]);
        setActivityCursor(p.next_cursor || "");
      }
    } catch {
      setError("Additional history could not be loaded.");
    } finally {
      setLoading(false);
    }
  }
  const rows = [
    ...events.map((e, index) => ({
      intermediate:
        e.type === "observation_changed" &&
        ["provisioning", "updating"].includes(e.phase) &&
        index > 0 &&
        e.generation === events[index - 1].generation &&
        e.phase === events[index - 1].phase &&
        !e.error &&
        !e.conditions.some(
          (c) =>
            c.type === "RouteVerified" &&
            !c.status &&
            c.message &&
            !c.message.startsWith("waiting"),
        ),
      key: `event-${e.id}`,
      at: e.occurred_at,
      operation: e.operation.id,
      generation: e.generation,
      title:
        e.type === "observation_changed"
          ? `Deployment ${e.phase}`
          : e.type.replaceAll("_", " "),
      detail:
        e.error?.message ||
        [...new Set(e.conditions.map((c) => c.message).filter(Boolean))].join(
          " · ",
        ),
      raw: e,
    })),
    ...activity.map((a) => ({
      key: `activity-${a.id}`,
      intermediate: false,
      at: a.occurred_at,
      operation: a.operation || "",
      generation: a.generation_to || 0,
      title: `${a.actor.display_name || a.actor.id} · ${activityLabel(a.action)} · ${a.outcome}`,
      detail: `via ${a.channel}`,
      raw: a,
    })),
  ].sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
  const groups = new Map<string, typeof rows>();
  for (const row of rows) {
    const key = `${row.generation}/${row.operation}`;
    groups.set(key, [...(groups.get(key) || []), row]);
  }
  return (
    <section className="envy-panel space-y-4" aria-label="Deployment history">
      <h2>Deployment history</h2>
      <p className="text-muted-foreground">
        Latest operations appear first. Expand deployment observations for
        startup details or raw records for the original evidence.
      </p>
      {isDemoMode ? (
        <p>Simulated preview · live deployment history unavailable.</p>
      ) : null}
      {loading && <p role="status">Loading history…</p>}
      {error && <p role="alert">{error}</p>}
      {!loading && !rows.length && !isDemoMode && !error && (
        <p>No retained history is available.</p>
      )}
      {Array.from(groups)
        .reverse()
        .map(([key, items]) => {
          const observations = items.filter((r) => r.intermediate);
          const milestones = items.filter((r) => !r.intermediate);
          const renderRow = (r: (typeof rows)[number]) => (
            <li key={r.key}>
              <strong>{r.title}</strong>
              <time>{new Date(r.at).toLocaleString()}</time>
              {r.detail && <p>{r.detail}</p>}
              <details>
                <summary>Raw record</summary>
                <pre className="envy-code">
                  {JSON.stringify(r.raw, null, 2)}
                </pre>
              </details>
            </li>
          );
          return (
            <section key={key} className="envy-history-group">
              <h3>Revision {items[0].generation || "unavailable"}</h3>
              <p className="text-muted-foreground">
                Operation <code>{items[0].operation || "unavailable"}</code>
              </p>
              <ol>{milestones.map(renderRow)}</ol>
              {!!observations.length && (
                <details>
                  <summary>
                    Intermediate deployment observations ({observations.length})
                  </summary>
                  <ol>{observations.map(renderRow)}</ol>
                </details>
              )}
            </section>
          );
        })}
      <div className="flex gap-2">
        {eventCursor && (
          <Button
            variant="outline"
            disabled={loading}
            onClick={() => void more("events")}
          >
            Load more lifecycle records
          </Button>
        )}
        {activityCursor && (
          <Button
            variant="outline"
            disabled={loading}
            onClick={() => void more("activity")}
          >
            Load older actor activity
          </Button>
        )}
      </div>
    </section>
  );
}
