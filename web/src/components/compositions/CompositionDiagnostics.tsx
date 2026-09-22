import { useEffect, useRef, useState } from "react";
import { useEnvyApi } from "../../context/ApiContext";
import { apiClient } from "../../lib/api-client";
import {
  Composition,
  ComponentLogs,
  LifecycleEvent,
  PageResponse,
} from "../../types/api";
import { Button } from "../ui/button";

export function CompositionDiagnostics({
  composition,
  initialComponent = "",
  hideHistory = false,
}: {
  composition: Composition;
  initialComponent?: string;
  hideHistory?: boolean;
}) {
  const { isDemoMode } = useEnvyApi();
  const [component, setComponent] = useState(
    initialComponent ||
      Object.keys(composition.overrides)[0] ||
      Object.keys(composition.components)[0] ||
      "",
  );
  const [logs, setLogs] = useState<ComponentLogs | null>(null);
  const [search, setSearch] = useState("");
  const [since, setSince] = useState(0);
  const [tail, setTail] = useState(200);
  const [bytes, setBytes] = useState(65536);
  const [copyNotice, setCopyNotice] = useState("");
  const [container, setContainer] = useState("");
  const [previous, setPrevious] = useState(false);
  const [events, setEvents] = useState<PageResponse<LifecycleEvent> | null>(
    null,
  );
  const [logError, setLogError] = useState("");
  const [eventError, setEventError] = useState("");
  const [loadingLogs, setLoadingLogs] = useState(false);
  const [loadingEvents, setLoadingEvents] = useState(false);
  const logRequest = useRef<AbortController | null>(null);
  const eventRequest = useRef<AbortController | null>(null);

  const shared = !composition.overrides[component];

  useEffect(() => {
    setContainer("");
    setPrevious(false);
    setLogs(null);
    setEvents(null);
    setLogError("");
    setEventError("");
    setLoadingLogs(false);
    setLoadingEvents(false);
    return () => {
      logRequest.current?.abort();
      eventRequest.current?.abort();
    };
  }, [composition.id, isDemoMode]);

  useEffect(() => {
    logRequest.current?.abort();
    setContainer("");
    setLogs(null);
    setLogError("");
    setLoadingLogs(false);
  }, [shared]);

  async function loadLogs() {
    logRequest.current?.abort();
    const controller = new AbortController();
    logRequest.current = controller;
    setLoadingLogs(true);
    setLogError("");
    setLogs(null);
    try {
      const result = await apiClient.getComponentLogs(
        composition.id,
        component,
        controller.signal,
        shared ? "" : container,
        previous,
        { tail_lines: tail, max_bytes: bytes, since_seconds: since },
      );
      if (!controller.signal.aborted) setLogs(result);
    } catch (error) {
      if (!controller.signal.aborted)
        setLogError(
          error instanceof Error ? error.message : "Unable to read logs",
        );
    } finally {
      if (!controller.signal.aborted) setLoadingLogs(false);
    }
  }

  async function loadEvents(after = "") {
    eventRequest.current?.abort();
    const controller = new AbortController();
    eventRequest.current = controller;
    setLoadingEvents(true);
    setEventError("");
    try {
      const result = await apiClient.listCompositionEvents(
        composition.id,
        after,
        controller.signal,
      );
      if (!controller.signal.aborted) setEvents(result);
    } catch (error) {
      if (!controller.signal.aborted)
        setEventError(
          error instanceof Error ? error.message : "Unable to read events",
        );
    } finally {
      if (!controller.signal.aborted) setLoadingEvents(false);
    }
  }

  if (isDemoMode) {
    return (
      <section className="rounded-lg border border-border p-3 text-xs text-muted-foreground">
        Component logs and lifecycle history are available in Live Mode.
      </section>
    );
  }

  return (
    <section
      aria-label="Composition diagnostics"
      className="space-y-4 border-t border-border pt-4 text-xs"
    >
      <div className="space-y-2">
        <h4 className="font-semibold text-foreground">Component logs</h4>
        <div className="flex flex-wrap items-center gap-2">
          <label htmlFor={`log-component-${composition.id}`}>Component</label>
          <select
            id={`log-component-${composition.id}`}
            value={component}
            disabled={loadingLogs}
            className="rounded border border-border bg-background px-2 py-1.5 text-foreground"
            onChange={(e) => {
              setComponent(e.target.value);
              setContainer("");
              setLogs(null);
              setLogError("");
            }}
          >
            {Object.keys(composition.components)
              .sort()
              .map((name) => (
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
          </select>
          {!shared && (
            <label className="flex items-center gap-2">
              Container
              <input
                className="envy-input"
                placeholder="Application (default)"
                value={container}
                maxLength={63}
                disabled={loadingLogs}
                onChange={(event) => {
                  setContainer(event.target.value);
                  setLogs(null);
                  setLogError("");
                }}
              />
            </label>
          )}
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={previous}
              disabled={loadingLogs}
              onChange={(event) => {
                setPrevious(event.target.checked);
                setLogs(null);
              }}
            />
            Previous container instance
          </label>
          <Button
            size="sm"
            variant="outline"
            disabled={
              loadingLogs ||
              composition.phase === "destroyed" ||
              !Number.isInteger(tail) ||
              tail < 1 ||
              tail > 1000
            }
            onClick={() => void loadLogs()}
          >
            {loadingLogs ? "Reading logs…" : "Read logs"}
          </Button>
        </div>
        <p className="text-muted-foreground">
          {shared
            ? "Shared-baseline logs include traffic from other compositions and baseline."
            : "Application logs from composition-owned workloads."}{" "}
          No request-level filtering. Up to {tail} lines per pod and{" "}
          {bytes / 1024} KiB total.
        </p>
        {composition.phase === "destroyed" && (
          <p className="text-muted-foreground">
            Pod logs are not retained after destruction.
          </p>
        )}
        <div className="flex flex-wrap gap-3">
          <label>
            Lookback
            <select
              className="envy-input"
              value={since}
              onChange={(e) => setSince(Number(e.target.value))}
            >
              <option value={0}>Available logs</option>
              <option value={300}>5 minutes</option>
              <option value={3600}>1 hour</option>
              <option value={86400}>24 hours</option>
            </select>
          </label>
          <label>
            Lines per pod
            <input
              className="envy-input"
              type="number"
              min={1}
              max={1000}
              value={tail}
              onChange={(e) => setTail(Number(e.target.value))}
            />
          </label>
          <label>
            Byte limit
            <select
              className="envy-input"
              value={bytes}
              onChange={(e) => setBytes(Number(e.target.value))}
            >
              <option value={65536}>64 KiB</option>
              <option value={262144}>256 KiB</option>
            </select>
          </label>
        </div>
        {logs && (
          <div className="space-y-2">
            <label>
              Search loaded snapshot
              <input
                className="envy-input"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
            </label>
            <div className="flex gap-2">
              <Button
                variant="outline"
                onClick={() => {
                  void navigator.clipboard
                    .writeText(
                      logs.streams
                        .map((s) => `${s.pod} / ${s.container}\n${s.text}`)
                        .join("\n"),
                    )
                    .then(() => setCopyNotice("Logs copied"))
                    .catch(() =>
                      setCopyNotice(
                        "Clipboard unavailable; select the log text manually.",
                      ),
                    );
                }}
              >
                Copy logs
              </Button>
              <Button
                variant="outline"
                onClick={() => {
                  const url = URL.createObjectURL(
                    new Blob(
                      [
                        logs.streams
                          .map((s) => `${s.pod} / ${s.container}\n${s.text}`)
                          .join("\n"),
                      ],
                      { type: "text/plain" },
                    ),
                  );
                  const a = document.createElement("a");
                  a.href = url;
                  a.download = `${composition.id}-${component}.log`;
                  a.click();
                  URL.revokeObjectURL(url);
                }}
              >
                Download snapshot
              </Button>
            </div>
            {copyNotice && <p role="status">{copyNotice}</p>}
          </div>
        )}
        {logError && (
          <p role="alert" className="text-red-300">
            {logError}
          </p>
        )}
        {logs && (
          <div aria-live="polite" className="space-y-2">
            <p className="text-muted-foreground">{logs.message}</p>
            {(logs.partial || logs.truncated) && (
              <p className="text-amber-300">
                {logs.partial && "Some pod logs could not be read. "}
                {logs.truncated &&
                  "The snapshot reached its byte or pod limit."}
              </p>
            )}
            {logs.streams.length === 0 && (
              <p className="text-muted-foreground">
                No pods with the selected container are currently available.
              </p>
            )}
            {logs.streams.map((stream) => (
              <div
                key={stream.workload_id}
                className="rounded border border-border p-2"
              >
                <p className="break-all font-mono text-muted-foreground">
                  {stream.pod} · {stream.container}
                </p>
                {stream.error ? (
                  <p className="text-amber-300">{stream.error.message}</p>
                ) : (
                  <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-all text-[11px]">
                    {(search
                      ? stream.text
                          .split("\n")
                          .filter((line) =>
                            line.toLowerCase().includes(search.toLowerCase()),
                          )
                          .join("\n")
                      : stream.text) ||
                      "No matching log lines in this snapshot."}
                  </pre>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
      {!hideHistory && (
        <div className="space-y-2">
          <h4 className="font-semibold text-foreground">Lifecycle history</h4>
          <p className="text-muted-foreground">
            Durable Envy events, oldest first. History remains after
            destruction.
          </p>
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={loadingEvents}
              onClick={() => void loadEvents()}
            >
              {loadingEvents
                ? "Loading events…"
                : events
                  ? "Refresh history"
                  : "Load history"}
            </Button>
            {events?.next_cursor && (
              <Button
                size="sm"
                variant="outline"
                disabled={loadingEvents}
                onClick={() => void loadEvents(events.next_cursor)}
              >
                Next event page
              </Button>
            )}
          </div>
          {eventError && (
            <p role="alert" className="text-red-300">
              {eventError}
            </p>
          )}
          {events && (
            <ol aria-live="polite" className="max-h-64 space-y-2 overflow-auto">
              {events.items.length === 0 && (
                <li className="text-muted-foreground">
                  No lifecycle events recorded.
                </li>
              )}
              {events.items.map((event) => (
                <li key={event.id} className="rounded border border-border p-2">
                  <div className="flex flex-wrap justify-between gap-1">
                    <span className="font-medium">
                      {event.type.replaceAll("_", " ")}
                    </span>
                    <span>
                      Gen {event.generation} · {event.phase}
                    </span>
                  </div>
                  <p className="text-muted-foreground">
                    {new Date(event.occurred_at).toLocaleString()} ·{" "}
                    {event.operation.kind}: {event.operation.status}
                  </p>
                  {event.type === "snapshot" && (
                    <p className="text-muted-foreground">
                      State recorded when event history was enabled.
                    </p>
                  )}
                  {event.error && (
                    <p className="text-amber-300">{event.error.message}</p>
                  )}
                </li>
              ))}
            </ol>
          )}
        </div>
      )}
    </section>
  );
}
