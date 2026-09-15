import { useEffect, useRef, useState } from "react";
import { Activity, Composition } from "../../types/api";
import { apiClient } from "../../lib/api-client";
import { useEnvyApi } from "../../context/ApiContext";
import { Button } from "../ui/button";

export function CompositionActivity({
  composition,
}: {
  composition: Composition;
}) {
  const { isDemoMode } = useEnvyApi();
  const [items, setItems] = useState<Activity[]>([]);
  const [next, setNext] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const request = useRef<AbortController | null>(null);

  useEffect(() => {
    setItems([]);
    setNext("");
    setError("");
    request.current?.abort();
    return () => request.current?.abort();
  }, [composition.id, isDemoMode]);

  async function load(after = "") {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setLoading(true);
    setError("");
    try {
      const page = await apiClient.listActivity(
        { resource_id: composition.id, after },
        controller.signal,
      );
      if (controller.signal.aborted) return;
      setItems((current) => (after ? [...current, ...page.items] : page.items));
      setNext(page.next_cursor || "");
    } catch (cause) {
      if (!controller.signal.aborted) {
        setError(
          cause instanceof Error ? cause.message : "Unable to load activity",
        );
      }
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  }

  if (isDemoMode) return null;

  return (
    <section
      aria-label="Composition activity"
      className="space-y-2 border-t border-border pt-4 text-xs"
    >
      <div className="flex items-center justify-between gap-3">
        <div>
          <h4 className="font-semibold text-foreground">
            Operational activity
          </h4>
          <p className="text-muted-foreground">
            Accepted requests and reconciler outcomes, with the verified actor
            and caller channel.
          </p>
        </div>
        <Button
          size="sm"
          variant="outline"
          disabled={loading}
          onClick={() => void load()}
        >
          {loading
            ? "Loading activity…"
            : items.length
              ? "Refresh activity"
              : "Load activity"}
        </Button>
      </div>
      {error && (
        <p role="alert" className="text-red-600 dark:text-red-300">
          {error}
        </p>
      )}
      {items.length > 0 && (
        <ol aria-live="polite" className="max-h-64 space-y-2 overflow-auto">
          {items.map((item) => (
            <li key={item.id} className="rounded border border-border p-2">
              <div className="flex flex-wrap justify-between gap-2">
                <span className="font-medium">
                  {item.action} · {item.outcome}
                </span>
                <time className="text-muted-foreground">
                  {new Date(item.occurred_at).toLocaleString()}
                </time>
              </div>
              <p className="text-muted-foreground">
                {item.actor.display_name || item.actor.id} via {item.channel}
                {item.generation_to
                  ? ` · generation ${item.generation_from || 0} → ${item.generation_to}`
                  : ""}
              </p>
              {item.operation && (
                <p className="font-mono text-muted-foreground">
                  Operation {item.operation}
                </p>
              )}
            </li>
          ))}
        </ol>
      )}
      {next && (
        <Button
          size="sm"
          variant="outline"
          disabled={loading}
          onClick={() => void load(next)}
        >
          Load older activity
        </Button>
      )}
    </section>
  );
}
