import React from "react";
import { FrontendBindings } from "./FrontendBindings";
import { CompositionDiagnostics } from "./CompositionDiagnostics";
import { CompositionRevisions } from "./CompositionRevisions";
import { CompositionActivity } from "./CompositionActivity";
import {
  ExternalLink,
  Copy,
  CheckCircle2,
  Clock,
  Pencil,
  Trash2,
} from "lucide-react";
import { Composition } from "../../types/api";
import {
  Dialog,
  DialogPortal,
  DialogBackdrop,
  DialogPopup,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "../ui/dialog";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { formatDate, formatTimeRemaining } from "../../lib/utils";

interface CompositionDetailModalProps {
  composition: Composition | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onUpdate: (comp: Composition) => void;
  onDestroy: (comp: Composition) => void;
}

export function CompositionDetailModal({
  composition,
  open,
  onOpenChange,
  onUpdate,
  onDestroy,
}: CompositionDetailModalProps) {
  const [copied, setCopied] = React.useState(false);

  if (!composition) return null;

  const copyUrl = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const copyCurl = () => {
    const url = composition.endpoints.public.url;
    const curl = `curl --resolve '${new URL(url).hostname}:8080:127.0.0.1' ${url}`;
    copyUrl(curl);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogPortal>
        <DialogBackdrop />
        <DialogPopup className="max-w-2xl">
          <DialogHeader>
            <div className="flex items-center justify-between pr-6">
              <div className="flex items-center gap-2">
                <DialogTitle>{composition.name}</DialogTitle>
                <Badge phase={composition.phase} />
              </div>
              <span className="text-xs font-mono text-muted-foreground">
                Gen {composition.generation} (Observed{" "}
                {composition.observed_generation})
              </span>
            </div>
            <DialogDescription className="font-mono text-xs">
              ID: {composition.id}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-5 py-2">
            {/* Endpoints Card */}
            <div className="p-4 rounded-lg bg-muted/40 border border-border space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  Public Preview Endpoint
                </span>
                {composition.endpoints.public.ready ? (
                  <span className="text-xs text-emerald-600 dark:text-emerald-400 flex items-center gap-1 font-medium">
                    <CheckCircle2 className="h-3.5 w-3.5" />{" "}
                    {composition.verification_level === "routing"
                      ? "Routing verified"
                      : "Endpoint reachable"}
                  </span>
                ) : (
                  <span className="text-xs text-amber-600 dark:text-amber-400 flex items-center gap-1 font-medium">
                    <Clock className="h-3.5 w-3.5" />{" "}
                    {["failed", "destroying", "destroyed"].includes(
                      composition.phase,
                    )
                      ? "Endpoint unavailable"
                      : "Verifying ingress…"}
                  </span>
                )}
              </div>

              <div className="flex items-center gap-2">
                <input
                  readOnly
                  value={composition.endpoints.public.url}
                  className="flex-1 bg-background border border-border px-3 py-1.5 rounded text-xs font-mono select-all text-foreground"
                />
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => copyUrl(composition.endpoints.public.url)}
                  title="Copy URL"
                  className="text-xs"
                >
                  <Copy className="h-3.5 w-3.5" />
                  {copied ? "Copied" : "Copy"}
                </Button>
                <Button
                  size="sm"
                  variant="default"
                  onClick={() =>
                    window.open(composition.endpoints.public.url, "_blank")
                  }
                  disabled={!composition.endpoints.public.ready}
                  className="text-xs"
                >
                  <ExternalLink className="h-3.5 w-3.5" />
                  Open
                </Button>
              </div>

              <div className="flex items-center justify-between text-[11px] text-muted-foreground pt-1">
                <span>DNS wildcard loopback format</span>
                <button
                  onClick={copyCurl}
                  className="text-foreground hover:underline font-medium flex items-center gap-1 cursor-pointer"
                >
                  Copy local loopback curl command
                </button>
              </div>
            </div>

            {/* Workload Components & Overrides */}
            <div className="space-y-2">
              <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                Composed Workload Topology
              </h4>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                {Object.entries(composition.components || {}).map(
                  ([name, comp]) => {
                    const isOverride = comp.source === "override";
                    return (
                      <div
                        key={name}
                        className={`p-3 rounded-lg border text-xs flex flex-col justify-between ${
                          isOverride
                            ? "border-zinc-400 bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-800/60 shadow-2xs"
                            : "border-border bg-card"
                        }`}
                      >
                        <div className="flex items-center justify-between mb-1">
                          <span className="font-semibold text-foreground capitalize">
                            {name}
                          </span>
                          <span
                            className={`text-[10px] px-1.5 py-0.5 rounded font-mono ${
                              isOverride
                                ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900 font-semibold"
                                : "bg-muted text-muted-foreground"
                            }`}
                          >
                            {comp.source}
                          </span>
                        </div>
                        <span
                          className="font-mono text-[11px] text-muted-foreground truncate"
                          title={comp.image}
                        >
                          {comp.image}
                        </span>
                        {comp.workload_id && (
                          <span className="text-[10px] text-muted-foreground/70 mt-1 truncate font-mono">
                            id: {comp.workload_id}
                          </span>
                        )}
                      </div>
                    );
                  },
                )}
              </div>
            </div>

            {/* Reconciliation Conditions */}
            <div className="space-y-2">
              <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                Lifecycle & Routing Conditions
              </h4>
              <div className="divide-y divide-border border border-border rounded-lg bg-card overflow-hidden text-xs">
                {composition.conditions.map((cond) => (
                  <div key={cond.type} className="p-3 flex items-start gap-3">
                    {cond.status ? (
                      <CheckCircle2 className="h-4 w-4 text-emerald-600 dark:text-emerald-400 shrink-0 mt-0.5" />
                    ) : (
                      <Clock className="h-4 w-4 text-amber-600 dark:text-amber-400 shrink-0 mt-0.5 animate-spin" />
                    )}
                    <div className="flex-1">
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
            {open && <FrontendBindings composition={composition} />}
            {open && <CompositionRevisions composition={composition} />}
            {open && <CompositionActivity composition={composition} />}
            {open && <CompositionDiagnostics composition={composition} />}

            {/* Meta & Expiry Info */}
            <div className="grid grid-cols-2 gap-4 text-xs bg-muted/40 p-3 rounded-lg border border-border">
              <div>
                <span className="text-muted-foreground block">Created:</span>
                <span className="font-mono font-medium text-foreground">
                  {formatDate(composition.created_at)}
                </span>
              </div>
              <div>
                <span className="text-muted-foreground block">Expires:</span>
                <span className="font-mono font-semibold text-amber-700 dark:text-amber-400">
                  {composition.phase === "destroyed"
                    ? "Destroyed"
                    : formatTimeRemaining(composition.expires_at)}
                </span>
              </div>
            </div>

            {/* Actions */}
            <div className="flex items-center justify-between pt-2 border-t border-border">
              <Button
                variant="destructive"
                size="sm"
                onClick={() => {
                  onOpenChange(false);
                  onDestroy(composition);
                }}
                disabled={
                  composition.phase === "destroying" ||
                  composition.phase === "destroyed"
                }
                className="text-xs gap-1.5"
              >
                <Trash2 className="h-3.5 w-3.5" />
                Destroy Composition
              </Button>

              <Button
                variant="default"
                size="sm"
                onClick={() => {
                  onOpenChange(false);
                  onUpdate(composition);
                }}
                disabled={
                  composition.phase === "destroying" ||
                  composition.phase === "destroyed"
                }
                className="text-xs gap-1.5 shadow-sm"
              >
                <Pencil className="h-3.5 w-3.5" />
                Update Image
              </Button>
            </div>
          </div>
        </DialogPopup>
      </DialogPortal>
    </Dialog>
  );
}
