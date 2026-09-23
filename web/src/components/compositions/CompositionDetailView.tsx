import { useEffect, useRef, useState } from "react";
import { ArrowLeft, Copy, ExternalLink, Settings2 } from "lucide-react";
import { FrontendBindings } from "./FrontendBindings";
import { CompositionDiagnostics } from "./CompositionDiagnostics";
import { DiagnosisSummary } from "./DiagnosisSummary";
import { CompositionRevisions } from "./CompositionRevisions";
import { CompositionHistory } from "./CompositionHistory";
import { PreviewEvidence, ExternalObservability } from "./PreviewEvidence";
import { PRPreviewOwnership } from "./PRPreviewOwnership";
import type { Composition, FrontendBindingView } from "../../types/api";
import { useEnvyApi } from "../../context/ApiContext";
import { apiClient } from "../../lib/api-client";
import {
  debugContext,
  lifecycleSummary,
  verificationSummary,
} from "../../lib/preview-presentation";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { formatDate, formatTimeRemaining } from "../../lib/utils";

const sections = ["Overview", "Changes", "Logs", "History"] as const;
export type PreviewSection = (typeof sections)[number];
export function CompositionDetailView({
  composition,
  onBack,
  onUpdate,
  onDestroy,
  section,
  onSectionChange,
}: {
  composition: Composition;
  onBack: () => void;
  onUpdate: (c: Composition) => void;
  onDestroy: (c: Composition) => void;
  section: PreviewSection;
  onSectionChange: (s: PreviewSection) => void;
}) {
  const { baselines, components, lastRefreshed, error, isDemoMode } =
    useEnvyApi();
  const baseline = baselines.find(
    (b) => b.project === composition.project && b.id === composition.baseline,
  );
  const [manage, setManage] = useState(false);
  const [selected, setSelected] = useState("");
  const [notice, setNotice] = useState("");
  const [debugText, setDebugText] = useState("");
  const [frontends, setFrontends] = useState<FrontendBindingView[]>([]);
  const [frontend, setFrontend] = useState("");
  const [bindingError, setBindingError] = useState("");
  const title = useRef<HTMLHeadingElement>(null);
  const detail = useRef<HTMLElement>(null);
  const inactive = ["destroyed", "destroying"].includes(composition.phase);
  useEffect(() => {
    title.current?.focus();
  }, [composition.id, section]);
  useEffect(() => {
    if (selected) detail.current?.focus();
  }, [selected]);
  useEffect(() => {
    const controller = new AbortController();
    setBindingError("");
    setFrontends([]);
    if (!isDemoMode)
      apiClient
        .listFrontendBindings(composition.id, controller.signal)
        .then((p) => {
          setFrontends(p.items);
          if (p.next_cursor)
            setBindingError(
              "Showing the first 100 application links. Use the CLI for more.",
            );
        })
        .catch(() => {
          if (!controller.signal.aborted)
            setBindingError("Application links could not be loaded.");
        });
    return () => controller.abort();
  }, [composition.id, composition.generation, isDemoMode]);
  const applications = frontends.filter((f) => f.binding.url);
  const application =
    applications.find(
      (f) => `${f.binding.frontend}/${f.binding.revision}` === frontend,
    ) || applications[0];
  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      setNotice("Copied to clipboard");
    } catch {
      setNotice("Clipboard unavailable. Select the displayed text to copy it.");
    }
  }
  async function copyDebug() {
    setNotice("Collecting debug context…");
    const result = JSON.parse(debugContext(composition, lastRefreshed));
    if (isDemoMode) {
      result.evidence = "Simulated preview; no live evidence";
      result.observability = "Unavailable in demo";
    } else {
      const [evidence, links] = await Promise.allSettled([
        apiClient.verification(composition.id),
        apiClient.observability(composition.id),
      ]);
      result.evidence =
        evidence.status === "fulfilled" ? evidence.value : "Unavailable";
      result.observability =
        links.status === "fulfilled" ? links.value.items : "Unavailable";
    }
    result.applications = bindingError
      ? "Unavailable"
      : frontends.map((f) => ({
          name: f.binding.frontend,
          commit: f.binding.revision,
          url: f.binding.url,
          check_state: f.check_state,
        }));
    const text = JSON.stringify(result, null, 2);
    setDebugText(text);
    await copy(text);
  }
  const observed = selected ? composition.components[selected] : undefined;
  const profile = components.find(
    (c) => c.project === composition.project && c.id === selected,
  );
  return (
    <div className="space-y-5">
      <Button variant="ghost" onClick={onBack}>
        <ArrowLeft />
        All previews
      </Button>
      <header className="envy-preview-heading">
        <div>
          <div className="flex flex-wrap items-center gap-3">
            <h1 ref={title} tabIndex={-1}>
              {composition.name}
            </h1>
            <Badge phase={composition.phase} />
          </div>
          <p>
            {composition.project} / {composition.baseline} ·{" "}
            {inactive
              ? "Preview ending or removed"
              : formatTimeRemaining(composition.expires_at)}
          </p>
          <p>
            {Object.entries(composition.overrides)
              .map(
                ([name, o]) =>
                  `${name}: ${o.source?.revision.slice(0, 8) || o.build_id || "direct image"}`,
              )
              .join(" · ")}
          </p>
          {composition.pr_preview_id && (
            <PRPreviewOwnership
              id={composition.pr_preview_id}
              compositionId={composition.id}
            />
          )}
        </div>
        <Button
          variant="outline"
          aria-expanded={manage}
          onClick={() => setManage(!manage)}
        >
          <Settings2 />
          Manage
        </Button>
      </header>
      <div className="envy-preview-actions">
        {applications.length > 1 && (
          <label>
            Application
            <select
              className="envy-input"
              value={
                application
                  ? `${application.binding.frontend}/${application.binding.revision}`
                  : ""
              }
              onChange={(e) => setFrontend(e.target.value)}
            >
              {applications.map((f) => (
                <option
                  key={`${f.binding.frontend}/${f.binding.revision}`}
                  value={`${f.binding.frontend}/${f.binding.revision}`}
                >
                  {f.binding.frontend} · {f.binding.revision.slice(0, 8)}
                </option>
              ))}
            </select>
          </label>
        )}
        {application && (
          <Button
            disabled={
              inactive ||
              !application.ready ||
              Date.parse(application.expires_at) <= Date.now()
            }
            onClick={() =>
              window.open(
                application.binding.url,
                "_blank",
                "noopener,noreferrer",
              )
            }
          >
            Open application
            <ExternalLink />
          </Button>
        )}
        <Button
          variant={application ? "outline" : "default"}
          disabled={inactive || !composition.endpoints.public.ready}
          onClick={() =>
            window.open(
              composition.endpoints.public.url,
              "_blank",
              "noopener,noreferrer",
            )
          }
        >
          Open API
          <ExternalLink />
        </Button>
        <Button variant="outline" onClick={() => void copyDebug()}>
          <Copy />
          Copy debug context
        </Button>
      </div>
      {application && (
        <p className="text-sm text-muted-foreground">
          Application commit:{" "}
          <code>{application.binding.revision.slice(0, 8)}</code> · Reported
          browser check:{" "}
          {application.binding.check
            ? `${application.binding.check.status} · ${inactive || !application.ready || Date.parse(application.expires_at) <= Date.now() ? "stale" : application.check_state} · tested revision ${application.binding.check.composition_generation} at ${formatDate(application.binding.check.reported_at)}`
            : "Not reported"}
        </p>
      )}
      {bindingError && <p role="status">{bindingError}</p>}
      {notice && <p role="status">{notice}</p>}
      {debugText && (
        <details>
          <summary>Debug context</summary>
          <pre className="envy-code">{debugText}</pre>
        </details>
      )}
      {manage && (
        <section className="envy-panel space-y-4" aria-label="Manage preview">
          <h2>Manage preview</h2>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              disabled={inactive || !!composition.pr_preview_id}
              onClick={() => onUpdate(composition)}
            >
              Update preview
            </Button>
            <Button
              variant="destructive"
              disabled={inactive}
              onClick={() => onDestroy(composition)}
            >
              Destroy preview
            </Button>
          </div>
          <FrontendBindings composition={composition} />
        </section>
      )}
      <div className="envy-preview-summary">
        <section
          className={`envy-outcome ${composition.phase === "failed" ? "needs-attention" : ""}`}
          aria-label="Preview outcome"
        >
          <Badge phase={composition.phase} />
          <h2>{lifecycleSummary(composition)}</h2>
          <p>{verificationSummary(composition)}</p>
          {(composition.last_error || composition.latest_operation.error) && (
            <p role="status">
              {
                (composition.last_error || composition.latest_operation.error)
                  ?.message
              }
            </p>
          )}
          <div className="envy-state-facts">
            <span>
              Requested revision <strong>{composition.generation}</strong>
            </span>
            <span>
              Observed revision{" "}
              <strong>{composition.observed_generation}</strong>
            </span>
            <span>
              {isDemoMode
                ? "Simulated preview"
                : `Last refreshed: ${lastRefreshed ? new Date(lastRefreshed).toLocaleTimeString() : "unavailable"}`}
            </span>
          </div>
          <p className="text-muted-foreground">
            Requested revision is the change you asked for. Observed revision is
            the latest deployment state Envy has seen. Check verification
            evidence in Overview to see which services actually answered a
            request.
          </p>
          {error && (
            <p role="alert">
              Refresh failed. Displayed information may be stale.
            </p>
          )}
        </section>
        <section className="envy-panel space-y-3">
          <h2>Preview details</h2>

          <label className="block">
            API URL
            <input
              aria-label="Preview URL"
              className="envy-input w-full"
              readOnly
              value={composition.endpoints.public.url}
            />
          </label>
          <Button
            variant="outline"
            onClick={() => void copy(composition.endpoints.public.url)}
          >
            Copy preview URL
          </Button>
          <dl className="envy-facts">
            <div>
              <dt>ID</dt>
              <dd>{composition.id}</dd>
            </div>
            <div>
              <dt>Baseline revision</dt>
              <dd>{composition.baseline_revision}</dd>
            </div>
            <div>
              <dt>Created</dt>
              <dd>{formatDate(composition.created_at)}</dd>
            </div>
            <div>
              <dt>Expires</dt>
              <dd>{formatDate(composition.expires_at)}</dd>
            </div>
          </dl>
          <details>
            <summary>Connection options</summary>
            {baseline?.routing?.preview_selector && (
              <div>
                <h3>Baseline endpoint selector</h3>
                <p>
                  Select this preview using{" "}
                  <code>{baseline.routing.preview_selector.header}</code>. The
                  selector is stripped before reaching the application.
                </p>
                <Button
                  variant="link"
                  disabled={inactive}
                  onClick={() =>
                    void copy(
                      `curl -H '${baseline.routing!.preview_selector!.header}: ${composition.id}' '${baseline.endpoint}'`,
                    )
                  }
                >
                  Copy baseline selector curl command
                </Button>
              </div>
            )}
            <div>
              <h3>Local development access</h3>
              <p>Only for an ingress exposed on this machine.</p>
              <Button
                variant="link"
                onClick={() => {
                  try {
                    const u = new URL(composition.endpoints.public.url);
                    void copy(
                      `curl --resolve '${u.hostname}:${u.port || (u.protocol === "https:" ? 443 : 80)}:127.0.0.1' '${u.href}'`,
                    );
                  } catch {
                    setNotice("Endpoint URL unavailable");
                  }
                }}
              >
                Copy local loopback curl command
              </Button>
            </div>
          </details>
        </section>
      </div>
      <nav className="envy-section-nav" aria-label="Preview sections">
        {sections.map((item) => (
          <button
            key={item}
            aria-current={section === item ? "page" : undefined}
            onClick={() => onSectionChange(item)}
          >
            {item}
          </button>
        ))}
      </nav>
      {section === "Overview" && (
        <>
          <DiagnosisSummary composition={composition} />
          <div className="space-y-5">
            <div className="space-y-5">
              <section className="envy-panel space-y-4">
                <h2>
                  {inactive
                    ? "Last recorded services"
                    : "What runs in this preview"}
                </h2>
                <p className="text-muted-foreground">
                  Your changed services run alongside the shared baseline.
                  Services pass along a preview label so downstream requests can
                  select your changes.
                </p>
                {(["override", "baseline"] as const).map((source) => (
                  <div key={source}>
                    <h3>
                      {source === "override"
                        ? "Your changes"
                        : "Shared with baseline"}
                    </h3>
                    <div className="envy-service-list">
                      {Object.entries(composition.components)
                        .filter(([, c]) => c.source === source)
                        .map(([name, c]) => (
                          <button
                            className={`envy-service-row ${selected === name ? "is-selected" : ""}`}
                            key={name}
                            aria-pressed={selected === name}
                            onClick={() => setSelected(name)}
                          >
                            <span>
                              <strong>{name}</strong>
                              <small>{c.image}</small>
                            </span>
                            <span>
                              {inactive
                                ? "Historical observation"
                                : c.execution_state ||
                                  c.status ||
                                  "State unavailable"}
                            </span>
                            <span aria-hidden="true">›</span>
                          </button>
                        ))}
                    </div>
                    {!Object.values(composition.components).some(
                      (c) => c.source === source,
                    ) && (
                      <p className="text-muted-foreground">
                        No services in this group.
                      </p>
                    )}
                  </div>
                ))}
              </section>
              <PreviewEvidence composition={composition} />
              <section className="envy-panel space-y-3">
                <h2>Sharing and dependencies</h2>
                <p>
                  Services marked shared use the live baseline. A separate
                  preview URL does not create separate databases or caches.
                </p>
                <p>
                  Data dependencies: unknown unless declared in the
                  application’s approved configuration.
                </p>
                {Object.entries(composition.preview_profiles || {}).map(
                  ([name, profile]) =>
                    profile.shared_dependencies?.length ? (
                      <p key={name}>
                        <strong>{name}</strong> shares:{" "}
                        {profile.shared_dependencies.join(", ")}
                      </p>
                    ) : null,
                )}
                <h3>
                  Message isolation:{" "}
                  {composition.message_isolation ? "Enabled" : "Disabled"}
                </h3>
                <p>
                  {composition.message_isolation
                    ? "Preview subscriptions are isolated. Application context propagation still needs verification. Expiry deletes remaining messages."
                    : "Messages follow baseline delivery. Preview consumer bindings are disabled."}
                </p>
                {composition.message_subscriptions?.map((s) => (
                  <details key={s.name}>
                    <summary>
                      {s.component} ·{" "}
                      {s.ready
                        ? "Subscription ready"
                        : "Subscription unavailable"}
                    </summary>
                    <p>
                      Topic: {s.topic} · Retention: {s.retention} · Expires{" "}
                      {formatDate(s.expires_at)}
                    </p>
                    <code>{s.name}</code>
                    <p>Filter: {s.filter}</p>
                    {s.backlog_may_be_lost && (
                      <p role="alert">
                        Subscription recreated; previously queued messages may
                        be lost.
                      </p>
                    )}
                    <p>
                      Inspect using your Google credentials. Pulling temporarily
                      leases messages and competes with a running worker;
                      acknowledging removes messages.
                    </p>
                    <code className="block wrap-anywhere">
                      gcloud pubsub subscriptions pull {s.name} --limit=10
                      --format=json
                    </code>
                  </details>
                ))}
              </section>
            </div>
          </div>

          {observed && (
            <section
              ref={detail}
              tabIndex={-1}
              className="envy-panel space-y-3"
              aria-label={`${selected} details`}
            >
              <div className="flex justify-between">
                <h2>{selected}</h2>
                <Button
                  variant="ghost"
                  onClick={() => {
                    setSelected("");
                    title.current?.focus();
                  }}
                >
                  Close details
                </Button>
              </div>
              <p>
                {observed.source === "override"
                  ? "Preview-owned workload"
                  : "Shared baseline workload"}{" "}
                ·{" "}
                {inactive
                  ? "Historical observation"
                  : observed.execution_state || observed.status}
              </p>
              <p className="font-mono wrap-anywhere">{observed.image}</p>
              <p>Workload identity: {observed.workload_id || "Unavailable"}</p>
              <p>
                Profile: {profile?.profile || "Unavailable"} · Protocol:{" "}
                {profile?.protocol || "Unavailable"}
              </p>
              <p>
                Declared execution dependencies:{" "}
                {profile?.execution?.dependencies?.join(", ") || "Unknown"}
              </p>
              <p>
                Registered baseline destination:{" "}
                {baseline?.components[selected]?.service_host || "Unavailable"}.
                Configuration does not prove a measured call path.
              </p>
              <Button onClick={() => onSectionChange("Logs")}>
                View {selected} logs
              </Button>
              <ExternalObservability id={composition.id} component={selected} />
            </section>
          )}
          <section className="envy-panel space-y-3">
            <h2>Need help?</h2>
            <div className="flex flex-wrap gap-2">
              {[
                ["My change isn’t showing", "Changes"],
                ["The endpoint won’t open", "Logs"],
                ["An API call fails", "Logs"],
                ["It worked before this update", "History"],
                ["My worker did nothing", "Logs"],
              ].map(([label, target]) => (
                <Button
                  variant="outline"
                  key={label}
                  onClick={() => onSectionChange(target as PreviewSection)}
                >
                  {label}
                </Button>
              ))}
            </div>
            <p className="text-muted-foreground">
              These shortcuts open relevant evidence; they are not automatic
              diagnoses.
            </p>
          </section>
          <section className="envy-panel">
            <ExternalObservability id={composition.id} />
          </section>
        </>
      )}
      {section === "Changes" && (
        <div className="space-y-5">
          <section className="envy-panel space-y-3">
            <h2>Requested source and artifacts</h2>
            {Object.entries(composition.overrides).map(([name, o]) => (
              <article key={name} className="envy-service-row">
                <div>
                  <h3>{name}</h3>
                  <p className="font-mono wrap-anywhere">
                    {o.image || o.build_id}
                  </p>
                  {o.source ? (
                    <>
                      <p>
                        {o.source.github_repository} · Commit{" "}
                        {o.source.revision}
                      </p>
                      <a
                        className="underline"
                        href={o.source.run_url}
                        target="_blank"
                        rel="noreferrer"
                      >
                        View CI run
                      </a>
                    </>
                  ) : (
                    <p>Direct image: source provenance unavailable</p>
                  )}
                </div>
              </article>
            ))}
          </section>
          <section className="envy-panel">
            <CompositionRevisions composition={composition} />
          </section>
        </div>
      )}
      {section === "Logs" && (
        <div className="space-y-5">
          <section className="envy-panel space-y-3">
            <h2>Health and routing details</h2>
            {inactive ? (
              <p>
                Preview resources are being removed or have been removed. False
                readiness conditions are expected during cleanup.
              </p>
            ) : null}
            {composition.conditions.map((c) => (
              <div key={c.type}>
                <strong>
                  {c.type} ·{" "}
                  {inactive
                    ? "Cleanup"
                    : c.status
                      ? "Satisfied"
                      : "Not verified"}
                </strong>
                <p>{c.message}</p>
              </div>
            ))}
          </section>
          <section className="envy-panel">
            <CompositionDiagnostics
              key={`${composition.id}/${selected}`}
              composition={composition}
              initialComponent={selected}
              hideHistory
            />
          </section>
        </div>
      )}
      {section === "History" && (
        <CompositionHistory composition={composition} />
      )}
    </div>
  );
}
