import { useEffect, useRef, useState } from "react";
import { FrontendBindings } from "./FrontendBindings";
import { CompositionDiagnostics } from "./CompositionDiagnostics";
import { CompositionRevisions } from "./CompositionRevisions";
import { CompositionActivity } from "./CompositionActivity";
import { PRPreviewOwnership } from "./PRPreviewOwnership";
import {
  ExternalLink,
  Copy,
  CheckCircle2,
  Clock,
  Pencil,
  Trash2,
  ArrowLeft,
  ShieldCheck,
} from "lucide-react";
import { Composition } from "../../types/api";
import { useEnvyApi } from "../../context/ApiContext";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { formatDate, formatTimeRemaining } from "../../lib/utils";

const sections = ["Overview", "Changes", "Diagnostics", "Activity"] as const;
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
  onUpdate: (composition: Composition) => void;
  onDestroy: (composition: Composition) => void;
  section: PreviewSection;
  onSectionChange: (section: PreviewSection) => void;
}) {
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState("");
  const { baselines } = useEnvyApi();
  const baseline = baselines.find(
    (candidate) =>
      candidate.project === composition.project &&
      candidate.id === composition.baseline,
  );
  const previewSelector = baseline?.routing?.preview_selector;
  const title = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    title.current?.focus();
  }, [composition.id]);
  const inactive = ["destroyed", "destroying"].includes(composition.phase);
  async function copyUrl(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setCopyError("");
    } catch {
      setCopyError("Could not copy. Select the URL and copy it manually.");
    }
  }
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 2000);
    return () => window.clearTimeout(timer);
  }, [copied]);
  const copyCurl = () => {
    try {
      const url = new URL(composition.endpoints.public.url);
      void copyUrl(
        `curl --resolve '${url.hostname}:${url.port || (url.protocol === "https:" ? "443" : "80")}:127.0.0.1' '${url.href}'`,
      );
    } catch {
      setCopyError("This preview does not yet have a valid endpoint URL.");
    }
  };
  const copySelectorCurl = () => {
    if (!baseline || !previewSelector) return;
    try {
      const url = new URL(baseline.endpoint);
      void copyUrl(
        `curl --resolve '${url.hostname}:${url.port || (url.protocol === "https:" ? "443" : "80")}:127.0.0.1' -H '${previewSelector.header}: ${composition.id}' '${url.href}'`,
      );
    } catch {
      setCopyError("This baseline does not yet have a valid endpoint URL.");
    }
  };
  return (
    <div className="space-y-6">
      <Button variant="ghost" onClick={onBack} className="-ml-3">
        <ArrowLeft />
        All previews
      </Button>
      <div className="envy-page-heading">
        <div>
          <span className="envy-eyebrow">Preview workspace</span>
          <h1 ref={title} tabIndex={-1}>
            {composition.name}
          </h1>
          <p className="font-mono wrap-anywhere">
            Composition {composition.id}
          </p>
          {composition.pr_preview_id && (
            <PRPreviewOwnership
              id={composition.pr_preview_id}
              compositionId={composition.id}
            />
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            onClick={() => onUpdate(composition)}
            disabled={inactive || !!composition.pr_preview_id}
          >
            <Pencil />
            Update preview
          </Button>
          <Button
            variant="ghost"
            className="text-destructive"
            onClick={() => onDestroy(composition)}
            disabled={inactive}
          >
            <Trash2 />
            Destroy
          </Button>
        </div>
      </div>
      <div className="envy-detail-meta">
        <Badge phase={composition.phase} />
        <span>
          Generation {composition.generation} · observed{" "}
          {composition.observed_generation}
        </span>
        <span className="flex items-center gap-2">
          <Clock size={14} />
          {composition.phase === "destroyed"
            ? "Destroyed"
            : formatTimeRemaining(composition.expires_at)}
        </span>
      </div>
      <section
        className="envy-panel p-4 space-y-3"
        aria-label="Message isolation"
      >
        <h2>
          Message isolation:{" "}
          {composition.message_isolation ? "Enabled" : "Disabled"}
        </h2>
        {composition.message_isolation ? (
          <p>
            Subscriptions can hold messages without a worker. Expiry deletes
            remaining messages. Infrastructure readiness does not verify
            application propagation.
          </p>
        ) : (
          <p>
            Messages follow baseline delivery. Preview consumer bindings are
            disabled.
          </p>
        )}
        {composition.message_subscriptions?.map((subscription) => (
          <div
            key={subscription.name}
            className="space-y-1 text-sm wrap-anywhere"
          >
            <p>
              <strong>{subscription.component}</strong> ·{" "}
              {subscription.ready
                ? "Subscription ready"
                : "Subscription unavailable"}
            </p>
            <p className="font-mono">{subscription.name}</p>
            <p>Topic: {subscription.topic}</p>
            <p>
              Filter: <code>{subscription.filter}</code>
            </p>
            <p>
              Retention:{" "}
              {subscription.retention === "604800s"
                ? "7 days"
                : subscription.retention}{" "}
              · Expires {formatDate(subscription.expires_at)}
            </p>
            {subscription.backlog_may_be_lost && (
              <p role="alert">
                Subscription recreated; previously queued messages may be lost.
              </p>
            )}
            <p>
              Inspect with your Google credentials (without automatic
              acknowledgement):
            </p>
            <code className="block">
              gcloud pubsub subscriptions pull {subscription.name} --limit=10
              --format=json
            </code>
            <p>
              Pulling temporarily leases messages and competes with a running
              worker. Acknowledging removes them from this subscription.
            </p>
          </div>
        ))}
      </section>
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
        <div className="envy-detail-layout">
          <div className="space-y-6 min-w-0">
            <section className="envy-panel">
              <div className="envy-section-heading">
                <h2>Your preview endpoint</h2>
                <p>A stable link to this composition’s selected services.</p>
              </div>
              <div className="envy-endpoint">
                <input
                  aria-label="Preview URL"
                  readOnly
                  value={composition.endpoints.public.url}
                />
                <Button
                  variant="outline"
                  onClick={() => void copyUrl(composition.endpoints.public.url)}
                  aria-label="Copy preview URL"
                >
                  <Copy />
                  {copied ? "Copied" : "Copy"}
                </Button>
                <Button
                  onClick={() =>
                    window.open(
                      composition.endpoints.public.url,
                      "_blank",
                      "noopener,noreferrer",
                    )
                  }
                  disabled={!composition.endpoints.public.ready || inactive}
                >
                  Open preview
                  <ExternalLink />
                </Button>
              </div>
              <div className="envy-verification-note">
                <ShieldCheck size={18} />
                <p>
                  {!composition.endpoints.public.ready || inactive
                    ? "Endpoint not ready. Check Diagnostics for workload and routing conditions."
                    : composition.verification_level === "routing"
                      ? "Request routing verified: checks confirm the intended overrides receive requests."
                      : composition.verification_level === "reachability"
                        ? "HTTP reachable. Request routing and context propagation are not verified."
                        : "Endpoint ready. Request routing and context propagation are not verified."}
                </p>
              </div>
              <details className="envy-advanced">
                <summary>Local development access</summary>
                <Button variant="link" size="sm" onClick={copyCurl}>
                  Copy local loopback curl command
                </Button>
              </details>
              {previewSelector && baseline && (
                <section className="envy-verification-note">
                  <ShieldCheck size={18} />
                  <div className="space-y-1">
                    <p>
                      Select this preview at the baseline endpoint with{" "}
                      <code>{previewSelector.header}</code>. This header is
                      stripped before your application receives the request.
                    </p>
                    <Button
                      variant="link"
                      size="sm"
                      onClick={copySelectorCurl}
                      disabled={inactive}
                    >
                      Copy baseline selector curl command
                    </Button>
                  </div>
                </section>
              )}
              {copyError && (
                <p role="alert" className="text-sm text-destructive">
                  {copyError}
                </p>
              )}
            </section>
            <section className="envy-panel">
              {" "}
              {/* Workload Components & Overrides */}
              <div className="space-y-2">
                <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  Shared foundation, selected changes
                </h2>
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                  {Object.entries(composition.components || {}).map(
                    ([name, comp]) => {
                      const isOverride = comp.source === "override";
                      return (
                        <div
                          key={name}
                          className={`p-3 rounded-lg border text-xs flex flex-col justify-between ${
                            isOverride
                              ? "border-primary/40 bg-accent shadow-2xs"
                              : "border-border bg-card"
                          }`}
                        >
                          <div className="flex flex-wrap gap-2 items-center justify-between mb-1">
                            <span className="font-semibold text-foreground wrap-anywhere">
                              {name}
                            </span>
                            <span
                              className={`text-[10px] px-1.5 py-0.5 rounded font-mono ${
                                isOverride
                                  ? "bg-primary text-primary-foreground font-semibold"
                                  : "bg-muted text-muted-foreground"
                              }`}
                            >
                              {isOverride ? "Override" : "Shared"}
                            </span>
                          </div>
                          <span
                            className="font-mono text-[11px] text-muted-foreground truncate"
                            title={comp.image}
                          >
                            {comp.image}
                          </span>
                          {comp.workload_id && (
                            <span className="text-[10px] text-muted-foreground mt-1 truncate font-mono">
                              id: {comp.workload_id}
                            </span>
                          )}
                        </div>
                      );
                    },
                  )}
                </div>
              </div>
            </section>
            <FrontendBindings composition={composition} />
          </div>
          <aside className="envy-panel self-start">
            <div className="envy-section-heading">
              <h2>At a glance</h2>
            </div>
            <dl className="envy-facts">
              <div>
                <dt>Project</dt>
                <dd>{composition.project}</dd>
              </div>
              <div>
                <dt>Baseline</dt>
                <dd>{composition.baseline}</dd>
              </div>
              <div>
                <dt>Baseline revision</dt>
                <dd>{composition.baseline_revision}</dd>
              </div>
              <div>
                <dt>Overrides</dt>
                <dd>
                  {Object.keys(composition.overrides).length}{" "}
                  {Object.keys(composition.overrides).length === 1
                    ? "service"
                    : "services"}
                </dd>
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
          </aside>
        </div>
      )}
      {section === "Changes" && (
        <div className="space-y-6">
          <section className="envy-panel">
            {" "}
            <div className="space-y-2">
              <h3 className="text-sm font-medium">
                Source revisions and artifacts
              </h3>
              {Object.entries(composition.overrides).map(
                ([component, override]) => (
                  <div
                    key={component}
                    className="rounded border border-border p-3 space-y-1 text-xs"
                  >
                    <p className="font-medium">{component}</p>
                    <p className="font-mono break-all">{override.image}</p>
                    {override.source ? (
                      <>
                        <p>{override.source.github_repository}</p>
                        <p className="font-mono break-all">
                          Git commit: {override.source.revision}
                        </p>
                        <p>
                          Built{" "}
                          {new Date(override.source.built_at).toLocaleString()}{" "}
                          · attempt {override.source.attempt}
                        </p>
                        <a
                          href={override.source.run_url}
                          target="_blank"
                          rel="noreferrer"
                          className="underline"
                        >
                          View CI run
                        </a>
                      </>
                    ) : (
                      <p className="text-muted-foreground">
                        Direct image: source provenance unavailable
                      </p>
                    )}
                  </div>
                ),
              )}
            </div>
          </section>
          <CompositionRevisions composition={composition} />
        </div>
      )}
      {section === "Diagnostics" && (
        <div className="space-y-6">
          <section className="envy-panel">
            {" "}
            {/* Reconciliation Conditions */}
            <div className="space-y-2">
              <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                Lifecycle & Routing Conditions
              </h2>
              <div className="divide-y divide-border border border-border rounded-lg bg-card overflow-hidden text-xs">
                {composition.conditions.map((cond) => (
                  <div key={cond.type} className="p-3 flex items-start gap-3">
                    {cond.status ? (
                      <CheckCircle2 className="h-4 w-4 text-emerald-600 dark:text-emerald-400 shrink-0 mt-0.5" />
                    ) : (
                      <Clock className="h-4 w-4 text-amber-600 dark:text-amber-400 shrink-0 mt-0.5" />
                    )}
                    <div className="flex-1 min-w-0 wrap-anywhere">
                      <div className="flex items-center justify-between">
                        <span className="font-medium text-foreground">
                          {cond.type}
                        </span>
                        <span
                          className={`text-[10px] font-mono font-semibold ${
                            cond.status
                              ? "text-emerald-600 dark:text-emerald-400"
                              : "text-amber-600 dark:text-amber-400"
                          }`}
                        >
                          {cond.status ? "True" : "False"}
                        </span>
                      </div>
                      <p className="text-muted-foreground text-[11px] mt-0.5">
                        {cond.message}
                      </p>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </section>
          <CompositionDiagnostics composition={composition} />
        </div>
      )}
      {section === "Activity" && (
        <section className="envy-panel">
          <CompositionActivity composition={composition} />
        </section>
      )}
    </div>
  );
}
