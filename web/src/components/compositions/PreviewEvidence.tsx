import { useEffect, useState } from "react";
import { apiClient, ApiRequestError } from "../../lib/api-client";
import { useEnvyApi } from "../../context/ApiContext";
import type {
  Composition,
  VerificationEvidence,
  ObservabilityLink,
} from "../../types/api";
import { Badge } from "../ui/badge";
import {
  Dialog,
  DialogTrigger,
  DialogPortal,
  DialogBackdrop,
  DialogPopup,
  DialogTitle,
} from "../ui/dialog";
import { Button } from "../ui/button";

export function ExternalObservability({
  id,
  component = "",
}: {
  id: string;
  component?: string;
}) {
  const { isDemoMode } = useEnvyApi();
  const [links, setLinks] = useState<ObservabilityLink[]>([]);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    setLinks([]);
    setError("");
    if (!isDemoMode)
      apiClient
        .observability(id, component, controller.signal)
        .then((r) => setLinks(r.items))
        .catch((e) => {
          if (!controller.signal.aborted)
            setError(
              e instanceof ApiRequestError && [404, 501].includes(e.status)
                ? "External links are not supported by this server."
                : "External links could not be loaded.",
            );
        });
    return () => controller.abort();
  }, [id, component, isDemoMode]);
  return (
    <section className="space-y-2">
      <h3>External observability</h3>
      {isDemoMode ? (
        <p>Simulated preview · external links unavailable.</p>
      ) : error ? (
        <p role="status">{error}</p>
      ) : links.length ? (
        links.map((link) => (
          <a
            className="envy-external-link"
            key={link.url}
            href={link.url}
            target="_blank"
            rel="noreferrer"
          >
            Open external {link.kind}: {link.label} ↗
          </a>
        ))
      ) : (
        <p className="text-muted-foreground">
          No links configured for this scope. Your operator can connect logs,
          traces, and dashboards in the server configuration.
        </p>
      )}
    </section>
  );
}
export function PreviewEvidence({ composition }: { composition: Composition }) {
  return <EvidenceList key={composition.id} composition={composition} />;
}
function EvidenceList({ composition }: { composition: Composition }) {
  const { isDemoMode } = useEnvyApi();
  const [items, setItems] = useState<VerificationEvidence[]>([]);
  const [next, setNext] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setError("");
    if (!isDemoMode) {
      setLoading(true);
      apiClient
        .verification(composition.id, "", controller.signal)
        .then((p) => {
          if (controller.signal.aborted) return;
          setItems(p.items);
          setNext(p.next_cursor || "");
        })
        .catch((e) => {
          if (!controller.signal.aborted)
            setError(
              e instanceof ApiRequestError && [404, 501].includes(e.status)
                ? "Verification evidence is not supported by this server."
                : "Verification evidence could not be loaded. Displayed checks may be stale.",
            );
        })
        .finally(() => {
          if (!controller.signal.aborted) setLoading(false);
        });
    }
    return () => controller.abort();
  }, [
    composition.id,
    composition.generation,
    composition.phase,
    isDemoMode,
    refresh,
  ]);
  async function more() {
    setLoading(true);
    try {
      const p = await apiClient.verification(composition.id, next);
      setItems((old) => [...old, ...p.items]);
      setNext(p.next_cursor || "");
    } catch {
      setError("Older verification evidence could not be loaded.");
    } finally {
      setLoading(false);
    }
  }
  const historicalOnly = ["destroyed", "destroying"].includes(
    composition.phase,
  );
  const current = historicalOnly
    ? undefined
    : items.find((item) => item.generation === composition.generation);
  const previous = items.filter((item) => item.id !== current?.id);
  return (
    <section
      className="envy-panel space-y-4"
      aria-label="Verification evidence"
    >
      <div className="envy-evidence-heading">
        <div>
          <h2>Verification evidence</h2>
          <p className="text-muted-foreground">
            Recorded requests, not comprehensive application tests.
          </p>
        </div>
        {!isDemoMode && (
          <Button
            variant="outline"
            disabled={loading}
            onClick={() => setRefresh((n) => n + 1)}
          >
            Refresh evidence
          </Button>
        )}
      </div>
      {error && <p role="status">{error}</p>}
      {isDemoMode ? (
        <p>Simulated preview · no real verification evidence.</p>
      ) : current ? (
        <>
          <CheckSummary item={current} />
          <EvidenceDialog item={current} technical />
        </>
      ) : (
        <p role="status">
          {loading && !items.length
            ? "Loading verification…"
            : historicalOnly
              ? "Preview removed or being removed · retained checks are historical."
              : !error
                ? "No verification recorded for this revision"
                : ""}
        </p>
      )}
      {!!previous.length && (
        <section className="space-y-3">
          <h3>Previous checks</h3>
          <table className="envy-check-table">
            <thead>
              <tr>
                <th>Outcome</th>
                <th>Check</th>
                <th>Revision</th>
                <th>Last checked</th>
                <th>
                  <span className="sr-only">Details</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {previous.map((item) => (
                <tr key={item.id}>
                  <td data-label="Outcome">
                    <Outcome item={item} />
                  </td>
                  <td data-label="Check">{checkName(item)}</td>
                  <td data-label="Revision">
                    {item.generation}
                    <span className="envy-history-label">
                      Historical
                      {item.generation === composition.generation &&
                      !historicalOnly
                        ? " · earlier check"
                        : ""}
                    </span>
                  </td>
                  <td data-label="Last checked">
                    <time dateTime={item.last_checked_at}>
                      {checkTime(item.last_checked_at)}
                    </time>
                  </td>
                  <td>
                    <EvidenceDialog item={item} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}
      {next && (
        <Button
          variant="outline"
          disabled={loading}
          onClick={() => void more()}
        >
          Load older checks
        </Button>
      )}
    </section>
  );
}
function checkTime(value: string) {
  return new Date(value).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    timeZoneName: "short",
  });
}
function checkName(item: VerificationEvidence) {
  return item.kind === "http" ? "HTTP reachability" : "Request routing";
}
function Outcome({ item }: { item: VerificationEvidence }) {
  return (
    <Badge variant={item.outcome === "passed" ? "success" : "destructive"}>
      {item.outcome === "passed" ? "Passed" : "Failed"}
    </Badge>
  );
}
function CheckSummary({
  item,
  historical = false,
}: {
  item: VerificationEvidence;
  historical?: boolean;
}) {
  return (
    <div className="envy-check-summary space-y-4">
      <div className="envy-evidence-heading">
        <div className="flex flex-wrap items-center gap-2">
          <h3>{checkName(item)}</h3>
          <Outcome item={item} />
          <Badge variant="secondary">Revision {item.generation}</Badge>
          <span className="text-sm text-muted-foreground">
            {historical ? "Historical check" : "Current revision"}
          </span>
        </div>
        <div className="envy-check-time">
          <span>Last checked</span>
          <time dateTime={item.last_checked_at}>
            {checkTime(item.last_checked_at)}
          </time>
        </div>
      </div>
      {item.error && (
        <p role="status" className="envy-check-error">
          {item.error.message}
        </p>
      )}
      <div className="envy-probe-grid">
        {item.probes.map((probe) => (
          <div key={probe.target} className="envy-probe">
            <span className="capitalize">{probe.target}</span>
            <strong>
              {probe.observed_status
                ? `HTTP ${probe.observed_status}`
                : "No HTTP response"}
            </strong>
            <small>Expected HTTP {probe.expected_status}</small>
          </div>
        ))}
      </div>
      {item.kind !== "http" && !!item.hops.length && (
        <div className="space-y-2">
          <h4>
            Observed request path
            {item.outcome !== "passed"
              ? " · incomplete or failed verification"
              : ""}
          </h4>
          <ol className="envy-evidence-path">
            {item.hops.map((hop, index) => (
              <li key={`${hop.service}-${index}`}>
                <strong>{hop.service}</strong>
                <code>{hop.version || "Version unavailable"}</code>
                <Badge variant="secondary">
                  {hop.deployment_composition === "baseline"
                    ? "Shared baseline"
                    : hop.deployment_composition
                      ? "Preview"
                      : "Ownership unavailable"}
                </Badge>
              </li>
            ))}
          </ol>
        </div>
      )}
    </div>
  );
}
function EvidenceDialog({
  item,
  technical = false,
}: {
  item: VerificationEvidence;
  technical?: boolean;
}) {
  return (
    <Dialog>
      <DialogTrigger
        render={<Button variant="outline" size="sm" />}
        aria-label={
          technical
            ? "Technical evidence"
            : `View check ${item.id}, revision ${item.generation}`
        }
      >
        {technical ? "Technical evidence" : "View"}
      </DialogTrigger>
      <DialogPortal>
        <DialogBackdrop />
        <DialogPopup className="envy-evidence-dialog">
          <DialogTitle>
            {technical ? "Technical evidence" : "Historical verification check"}
          </DialogTitle>
          {!technical && <CheckSummary item={item} historical />}
          <dl className="envy-facts">
            <div>
              <dt>First checked</dt>
              <dd>{checkTime(item.first_checked_at)}</dd>
            </div>
            <div>
              <dt>Last checked</dt>
              <dd>{checkTime(item.last_checked_at)}</dd>
            </div>
          </dl>
          {!!item.hops.length && (
            <section>
              <h3>Workload identities</h3>
              <dl className="envy-facts">
                {item.hops.map((hop, index) => (
                  <div key={index}>
                    <dt>{hop.service}</dt>
                    <dd>
                      <code>{hop.workload_id || "Unavailable"}</code>
                    </dd>
                  </div>
                ))}
              </dl>
            </section>
          )}
          <section>
            <h3>Original check record</h3>
            <pre className="envy-code">{JSON.stringify(item, null, 2)}</pre>
          </section>
        </DialogPopup>
      </DialogPortal>
    </Dialog>
  );
}
