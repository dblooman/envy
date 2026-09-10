import React, { useState } from "react";
import {
  ExternalLink,
  Copy,
  Clock,
  Pencil,
  Trash2,
  Eye,
  Check,
  CheckCircle2,
  AlertTriangle,
} from "lucide-react";
import { Composition } from "../../types/api";
import { Card, CardHeader, CardContent, CardFooter } from "../ui/card";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { formatTimeRemaining, cn } from "../../lib/utils";

interface CompositionCardProps {
  composition: Composition;
  onInspect: (comp: Composition) => void;
  onUpdate: (comp: Composition) => void;
  onDestroy: (comp: Composition) => void;
}

export function CompositionCard({
  composition,
  onInspect,
  onUpdate,
  onDestroy,
}: CompositionCardProps) {
  const [copied, setCopied] = useState(false);
  const isTerminal =
    composition.phase === "destroyed" || composition.phase === "failed";
  const isPending =
    composition.phase === "provisioning" || composition.phase === "updating";

  const copyUrl = (e: React.MouseEvent) => {
    e.stopPropagation();
    navigator.clipboard.writeText(composition.endpoints.public.url);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Card className="hover:border-zinc-400 dark:hover:border-zinc-600 transition-all duration-200 shadow-2xs hover:shadow-xs flex flex-col justify-between group">
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between gap-3">
          <div className="space-y-1 min-w-0 flex-1">
            <h3
              onClick={() => onInspect(composition)}
              className="font-semibold text-sm sm:text-base text-foreground truncate cursor-pointer hover:text-primary transition-colors flex items-center gap-2"
            >
              {composition.name}
            </h3>
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground font-mono whitespace-nowrap overflow-hidden">
              <span className="shrink-0">Gen {composition.generation}</span>
              <span className="shrink-0 text-muted-foreground/50">•</span>
              <span className="truncate" title={composition.id}>
                {composition.id.slice(0, 16)}...
              </span>
            </div>
          </div>
          <Badge phase={composition.phase} className="shrink-0" />
        </div>
      </CardHeader>

      <CardContent className="space-y-3 pb-3 text-xs">
        {/* Override Pill */}
        <div className="p-2.5 rounded-lg bg-muted/50 border border-border/80 space-y-1.5">
          <div className="flex items-center justify-between text-[11px] text-muted-foreground font-medium">
            <span>Override Workload</span>
            <span className="text-foreground font-semibold">
              {Object.keys(composition.overrides).sort().join(", ")}
            </span>
          </div>
          <div className="font-mono text-[11px] text-foreground font-medium truncate bg-background px-2 py-1 rounded border border-border/60">
            {Object.entries(composition.overrides)
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([id, o]) => (
                <div key={id} title={o.image} className="truncate">
                  {id}: {o.image}
                </div>
              ))}
          </div>
        </div>

        {composition.verification_level === "reachability" && (
          <p className="text-xs text-amber-700 dark:text-amber-300">
            HTTP checks passed. Request routing and context propagation are not
            verified.
          </p>
        )}
        {/* Public Endpoint */}
        <div className="space-y-1">
          <div className="flex items-center justify-between text-[11px] text-muted-foreground">
            <span className="flex items-center gap-1">
              {composition.endpoints.public.ready ? (
                <span className="text-emerald-600 dark:text-emerald-400 flex items-center gap-1 font-medium">
                  <CheckCircle2 className="h-3 w-3" />{" "}
                  {composition.verification_level === "reachability"
                    ? "HTTP reachable"
                    : "Ready"}
                </span>
              ) : isPending ? (
                <span className="text-amber-600 dark:text-amber-400 flex items-center gap-1 font-medium">
                  <Clock className="h-3 w-3 animate-spin" /> Provisioning route
                </span>
              ) : (
                <span className="text-zinc-500 flex items-center gap-1">
                  <AlertTriangle className="h-3 w-3" /> Not ready
                </span>
              )}
            </span>
            <span className="text-muted-foreground flex items-center gap-1">
              <Clock className="h-3 w-3" />
              {formatTimeRemaining(composition.expires_at)}
            </span>
          </div>

          <div className="flex items-center gap-1.5 bg-background border border-border rounded-md px-2.5 py-1 font-mono text-[11px] text-foreground">
            <span
              className="truncate flex-1"
              title={composition.endpoints.public.url}
            >
              {composition.endpoints.public.url}
            </span>
            <button
              onClick={copyUrl}
              className="text-muted-foreground hover:text-foreground p-1 transition-colors cursor-pointer"
              title="Copy URL"
            >
              {copied ? (
                <Check className="h-3.5 w-3.5 text-emerald-600 dark:text-emerald-400" />
              ) : (
                <Copy className="h-3.5 w-3.5" />
              )}
            </button>
          </div>
        </div>
      </CardContent>

      <CardFooter className="pt-2 border-t border-border/60 flex items-center justify-between gap-2">
        <div className="flex items-center gap-1">
          <Button
            size="sm"
            variant="ghost"
            onClick={() => onInspect(composition)}
            className="h-8 px-2.5 text-xs text-muted-foreground hover:text-foreground gap-1"
          >
            <Eye className="h-3.5 w-3.5" />
            <span>Inspect</span>
          </Button>

          {!isTerminal && (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => onUpdate(composition)}
              className="h-8 px-2.5 text-xs text-muted-foreground hover:text-foreground gap-1"
            >
              <Pencil className="h-3.5 w-3.5" />
              <span>Update</span>
            </Button>
          )}
        </div>

        <div className="flex items-center gap-1">
          {!isTerminal && (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => onDestroy(composition)}
              className="h-8 px-2 text-xs text-muted-foreground hover:text-destructive hover:bg-destructive/10"
              title="Destroy preview"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          )}

          <Button
            size="sm"
            variant={composition.endpoints.public.ready ? "default" : "outline"}
            onClick={() =>
              window.open(composition.endpoints.public.url, "_blank")
            }
            disabled={!composition.endpoints.public.ready}
            className={cn(
              "h-8 px-3 text-xs gap-1",
              !composition.endpoints.public.ready &&
                "text-muted-foreground opacity-60 border-border cursor-not-allowed hover:bg-transparent",
            )}
          >
            <span>Open</span>
            <ExternalLink className="h-3 w-3" />
          </Button>
        </div>
      </CardFooter>
    </Card>
  );
}
