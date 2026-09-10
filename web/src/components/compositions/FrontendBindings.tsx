import { useEffect, useState } from "react";
import { useEnvyApi } from "../../context/ApiContext";
import { apiClient } from "../../lib/api-client";
import { Composition, FrontendBindingView } from "../../types/api";
import { Button } from "../ui/button";

export function FrontendBindings({ composition }: { composition: Composition }) {
  const { isDemoMode, serverUrl, token } = useEnvyApi();
  const [items, setItems] = useState<FrontendBindingView[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setItems([]); setError(""); setLoading(!isDemoMode);
    if (!isDemoMode) apiClient.listFrontendBindings(composition.id, controller.signal)
      .then(page => { if (!controller.signal.aborted) { setItems(page.items); if (page.next_cursor) setError("Showing the first 100 bindings. Use the CLI to inspect additional pages."); } })
      .catch(err => { if (!controller.signal.aborted) setError(err instanceof Error ? err.message : "Unable to load bindings"); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [composition.id, composition.generation, composition.phase, isDemoMode, serverUrl, token, refresh]);
  return <section className="space-y-3 rounded-lg border border-border p-3 text-xs">
    <div className="flex items-center justify-between"><h4 className="font-semibold">Frontend bindings</h4>
      {!isDemoMode && <Button size="sm" variant="outline" onClick={() => setRefresh(n => n + 1)} disabled={loading}>Refresh</Button>}
    </div>
    <p className="text-muted-foreground">Frontend deployment links and browser checks are reported by callers, separately from backend readiness.</p>
    {isDemoMode ? <p>Bindings are available in Live Mode.</p> : loading ? <p>Loading bindings…</p> : !items.length && !error ? <p>No frontend revisions bound. Use delivery frontend bind or the bind_frontend MCP tool.</p> : null}
    {error && <p role="alert" className="text-red-600">{error}</p>}
    {items.map(({ binding: b, ready, check_state, composition_generation, expires_at }) => {
      const available = ready && Date.parse(expires_at) > Date.now();
      const state = !available && check_state === "current" ? "stale" : check_state;
      return <article key={`${b.frontend}/${b.revision}`} className="space-y-2 rounded border border-border p-3 break-words">
        <div className="flex justify-between gap-2"><strong>{b.frontend}</strong><span>Binding v{b.version}</span></div>
        <a href={b.repository} target="_blank" rel="noopener noreferrer" className="underline">{b.repository}</a>
        <p className="font-mono text-[11px] break-all">{b.revision}</p>
        <p>Backend: {available ? "ready" : "unavailable"} · Generation {composition_generation}</p>
        {b.url ? <p>Reported frontend: <a href={b.url} target="_blank" rel="noopener noreferrer" className="underline">{b.url}</a></p> : <p>Frontend URL not reported.</p>}
        <p>Browser check: {b.check ? `${b.check.status} · ${state}` : "not reported"}</p>
        {b.check && <p className="text-muted-foreground">{b.check.message} · Tested generation {b.check.composition_generation} · {new Date(b.check.reported_at).toLocaleString()}</p>}
      </article>;
    })}
  </section>;
}
