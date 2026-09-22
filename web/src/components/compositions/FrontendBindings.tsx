import { useEffect, useState } from "react";
import { useEnvyApi } from "../../context/ApiContext";
import { apiClient } from "../../lib/api-client";
import { Composition, FrontendBindingView } from "../../types/api";
import { Button } from "../ui/button";
import { Input } from "../ui/input";

export function FrontendBindings({
  composition,
}: {
  composition: Composition;
}) {
  const { isDemoMode } = useEnvyApi();
  const [items, setItems] = useState<FrontendBindingView[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [refresh, setRefresh] = useState(0);
  const [frontend, setFrontend] = useState("");
  const [revision, setRevision] = useState("");
  const [repository, setRepository] = useState("");
  const [actionBusy, setActionBusy] = useState(false);
  const [resolution, setResolution] = useState("");
  const updateItem = (next: FrontendBindingView) =>
    setItems((old) => {
      const filtered = old.filter(
        (x) =>
          !(
            x.binding.frontend === next.binding.frontend &&
            x.binding.revision === next.binding.revision
          ),
      );
      return [next, ...filtered];
    });
  async function bind() {
    setActionBusy(true);
    setError("");
    try {
      updateItem(
        await apiClient.bindFrontend(
          composition.project,
          frontend.trim(),
          revision.trim(),
          composition.id,
          repository.trim(),
        ),
      );
      setFrontend("");
      setRevision("");
      setRepository("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to bind frontend");
    } finally {
      setActionBusy(false);
    }
  }
  async function publish(b: FrontendBindingView["binding"]) {
    const input = document.getElementById(
      `url-${b.frontend}-${b.revision}`,
    ) as HTMLInputElement;
    setActionBusy(true);
    setError("");
    try {
      updateItem(
        await apiClient.publishFrontend(
          b.project,
          b.frontend,
          b.revision,
          b.version,
          input.value,
        ),
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to record URL");
    } finally {
      setActionBusy(false);
    }
  }
  async function check(
    b: FrontendBindingView["binding"],
    status: "passed" | "failed",
  ) {
    const input = document.getElementById(
      `check-${b.frontend}-${b.revision}`,
    ) as HTMLInputElement;
    setActionBusy(true);
    setError("");
    try {
      updateItem(
        await apiClient.checkFrontend(
          b.project,
          b.frontend,
          b.revision,
          b.version,
          composition.generation,
          status,
          input.value,
        ),
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to record check");
    } finally {
      setActionBusy(false);
    }
  }
  async function resolve(b: FrontendBindingView["binding"]) {
    setActionBusy(true);
    setError("");
    try {
      const out = await apiClient.resolveFrontend(
        b.project,
        b.frontend,
        b.revision,
      );
      setResolution(
        `${b.frontend}@${b.revision.slice(0, 8)} resolves to ${out.api_url} at generation ${out.composition_generation}`,
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to resolve frontend");
    } finally {
      setActionBusy(false);
    }
  }
  useEffect(() => {
    const controller = new AbortController();
    setItems([]);
    setError("");
    setLoading(!isDemoMode);
    if (!isDemoMode)
      apiClient
        .listFrontendBindings(composition.id, controller.signal)
        .then((page) => {
          if (!controller.signal.aborted) {
            setItems(page.items);
            if (page.next_cursor)
              setError(
                "Showing the first 100 bindings. Use the CLI to inspect additional pages.",
              );
          }
        })
        .catch((err) => {
          if (!controller.signal.aborted)
            setError(
              err instanceof Error ? err.message : "Unable to load bindings",
            );
        })
        .finally(() => {
          if (!controller.signal.aborted) setLoading(false);
        });
    return () => controller.abort();
  }, [
    composition.id,
    composition.generation,
    composition.phase,
    isDemoMode,
    refresh,
  ]);
  return (
    <section className="space-y-3 rounded-lg border border-border p-3 text-xs">
      <div className="flex items-center justify-between">
        <h4 className="font-semibold">Frontend bindings</h4>
        {!isDemoMode && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => setRefresh((n) => n + 1)}
            disabled={loading}
          >
            Refresh
          </Button>
        )}
      </div>
      <p className="text-muted-foreground">
        Frontend deployment links and browser checks are reported by callers,
        separately from backend readiness.
      </p>
      {!isDemoMode &&
        !["destroying", "destroyed"].includes(composition.phase) && (
          <div className="grid gap-2 rounded border border-border bg-muted/30 p-3 sm:grid-cols-3">
            <Input
              aria-label="Frontend name"
              value={frontend}
              onChange={(e) => setFrontend(e.target.value)}
              placeholder="Frontend name"
            />
            <Input
              aria-label="Frontend revision"
              value={revision}
              onChange={(e) => setRevision(e.target.value)}
              placeholder="Full commit SHA"
            />
            <Input
              aria-label="Frontend repository"
              value={repository}
              onChange={(e) => setRepository(e.target.value)}
              placeholder="https://github.com/org/repo"
            />
            <Button
              className="sm:col-span-3"
              variant="outline"
              disabled={actionBusy || !frontend || !revision || !repository}
              onClick={() => void bind()}
            >
              Bind frontend revision
            </Button>
          </div>
        )}
      {isDemoMode ? (
        <p>Bindings are available in Live Mode.</p>
      ) : loading ? (
        <p>Loading bindings…</p>
      ) : !items.length && !error ? (
        <p>
          No frontend revisions bound. Use envy frontend bind or the
          bind_frontend MCP tool.
        </p>
      ) : null}
      {error && (
        <p role="alert" className="text-red-600">
          {error}
        </p>
      )}
      {items.map(
        ({
          binding: b,
          ready,
          check_state,
          composition_generation,
          expires_at,
        }) => {
          const available = ready && Date.parse(expires_at) > Date.now();
          const state =
            !available && check_state === "current" ? "stale" : check_state;
          return (
            <article
              key={`${b.frontend}/${b.revision}`}
              className="space-y-2 rounded border border-border p-3 wrap-break-word"
            >
              <div className="flex justify-between gap-2">
                <strong>{b.frontend}</strong>
                <span>Binding v{b.version}</span>
              </div>
              <a
                href={b.repository}
                target="_blank"
                rel="noopener noreferrer"
                className="underline"
              >
                {b.repository}
              </a>
              <p className="font-mono text-[11px] break-all">{b.revision}</p>
              <p>
                Backend: {available ? "ready" : "unavailable"} · Generation{" "}
                {composition_generation}
              </p>
              <Button
                size="sm"
                variant="outline"
                disabled={!available || actionBusy}
                onClick={() => void resolve(b)}
              >
                Resolve binding
              </Button>
              {b.url ? (
                <p>
                  Reported frontend:{" "}
                  <a
                    href={b.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="underline"
                  >
                    {b.url}
                  </a>
                </p>
              ) : (
                <p>Frontend URL not reported.</p>
              )}
              {!isDemoMode && (
                <div className="grid gap-2 border-t border-border pt-2 sm:grid-cols-[1fr_auto]">
                  <Input
                    aria-label={`Published URL for ${b.frontend}`}
                    defaultValue={b.url || ""}
                    placeholder="https://preview.example.com"
                    id={`url-${b.frontend}-${b.revision}`}
                  />
                  <Button
                    variant="outline"
                    disabled={actionBusy}
                    onClick={() => void publish(b)}
                  >
                    Record URL
                  </Button>
                  {b.url && (
                    <>
                      <Input
                        aria-label={`Check message for ${b.frontend}`}
                        placeholder="Homepage and API calls verified"
                        id={`check-${b.frontend}-${b.revision}`}
                      />
                      <div className="flex gap-1">
                        <Button
                          variant="outline"
                          disabled={actionBusy}
                          onClick={() => void check(b, "passed")}
                        >
                          Report pass
                        </Button>
                        <Button
                          variant="outline"
                          disabled={actionBusy}
                          onClick={() => void check(b, "failed")}
                        >
                          Report failure
                        </Button>
                      </div>
                    </>
                  )}
                </div>
              )}
              <p>
                Reported browser check:{" "}
                {b.check ? `${b.check.status} · ${state}` : "not reported"}
              </p>
              {b.check && (
                <p className="text-muted-foreground">
                  {b.check.message} · Tested generation{" "}
                  {b.check.composition_generation} ·{" "}
                  {new Date(b.check.reported_at).toLocaleString()}
                </p>
              )}
            </article>
          );
        },
      )}
      {resolution && (
        <p
          role="status"
          className="rounded border border-emerald-300 bg-emerald-50 p-2 text-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-200"
        >
          {resolution}
        </p>
      )}
    </section>
  );
}
