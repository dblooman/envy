import { Button } from "../ui/button";
import { useEffect, useState } from "react";
import { apiClient } from "../../lib/api-client";
import { Composition, CompositionRevision } from "../../types/api";
import { useEnvyApi } from "../../context/ApiContext";

export function CompositionRevisions({
  composition,
}: {
  composition: Composition;
}) {
  const { isDemoMode } = useEnvyApi();
  const [items, setItems] = useState<CompositionRevision[]>([]);
  const [error, setError] = useState("");
  const [next, setNext] = useState("");
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    if (isDemoMode) return;
    const controller = new AbortController();
    setLoading(true);
    setError("");
    setNext("");
    setItems([]);
    apiClient
      .listRevisions(composition.id, controller.signal)
      .then((p) => {
        if (!controller.signal.aborted) {
          setItems(p.items);
          setNext(p.next_cursor || "");
        }
      })
      .catch((e) => {
        if (!controller.signal.aborted)
          setError(e instanceof Error ? e.message : "Unable to load revisions");
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [composition.id, composition.generation, isDemoMode]);
  async function more() {
    setLoading(true);
    try {
      const p = await apiClient.listRevisions(composition.id, undefined, next);
      setItems((old) => [...old, ...p.items]);
      setNext(p.next_cursor || "");
    } catch {
      setError("Additional revisions could not be loaded.");
    } finally {
      setLoading(false);
    }
  }
  return (
    <section className="space-y-2">
      <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        Requested revisions
      </h4>
      {error && (
        <p role="alert" className="text-xs text-rose-600">
          {error}
        </p>
      )}
      {loading && <p role="status">Loading revisions…</p>}
      {isDemoMode ? (
        <p className="text-xs text-muted-foreground">
          Revision history is available in Live Mode.
        </p>
      ) : !loading && !error && items.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          No revision snapshots are available for this older composition.
        </p>
      ) : (
        <ol className="space-y-2">
          {items.map((item, index) => {
            const previous = items[index - 1];
            const removed = Object.keys(previous?.overrides || {}).filter(
              (name) => !item.overrides[name],
            );
            return (
              <li
                key={item.generation}
                className="rounded border border-border p-3 text-xs"
              >
                <div className="flex flex-wrap justify-between gap-2">
                  <strong>Revision {item.generation}</strong>
                  <span>
                    {item.actor.display_name || item.actor.id} · {item.channel}{" "}
                    · {new Date(item.created_at).toLocaleString()}
                  </span>
                </div>
                <div className="mt-2 grid gap-1 font-mono">
                  {removed.map((name) => (
                    <span key={name}>
                      {name}: removed · now shared with baseline
                    </span>
                  ))}
                  {Object.entries(item.overrides).map(
                    ([component, override]) => {
                      const old = previous?.overrides[component];
                      const ref = override.build_id
                        ? `build:${override.build_id}`
                        : override.image;
                      const oldRef = old?.build_id
                        ? `build:${old.build_id}`
                        : old?.image;
                      return (
                        <span
                          key={component}
                          className={
                            oldRef && oldRef !== ref
                              ? "text-amber-700 dark:text-amber-300"
                              : "text-muted-foreground"
                          }
                        >
                          {component}
                          {previous && !old ? " (added)" : ""}:{" "}
                          {oldRef && oldRef !== ref ? `${oldRef} → ` : ""}
                          {ref}
                        </span>
                      );
                    },
                  )}
                </div>
              </li>
            );
          })}
        </ol>
      )}
      {next && (
        <Button
          variant="outline"
          disabled={loading}
          onClick={() => void more()}
        >
          Load more revisions
        </Button>
      )}
    </section>
  );
}
