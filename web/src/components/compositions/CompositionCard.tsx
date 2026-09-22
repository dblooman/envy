import { useEffect, useState } from "react";
import {
  ExternalLink,
  Copy,
  Clock,
  Check,
  ShieldCheck,
  Layers3,
  Box,
} from "lucide-react";
import { Composition } from "../../types/api";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { formatTimeRemaining } from "../../lib/utils";
interface CompositionCardProps {
  composition: Composition;
  onInspect: (comp: Composition) => void;
  onUpdate: (comp: Composition) => void;
  onDestroy: (comp: Composition) => void;
}
export function CompositionCard({
  composition,
  onInspect,
}: CompositionCardProps) {
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState("");
  const inactive = ["destroyed", "destroying"].includes(composition.phase);
  const services = Object.keys(composition.overrides).sort();
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 2000);
    return () => window.clearTimeout(timer);
  }, [copied]);
  async function copyUrl() {
    try {
      await navigator.clipboard.writeText(composition.endpoints.public.url);
      setCopied(true);
      setCopyError("");
    } catch {
      setCopyError(
        "Could not copy this URL. Open the preview details to select it.",
      );
    }
  }
  return (
    <article className="envy-preview-card">
      <div className="envy-card-main">
        <div className="envy-card-top">
          <span className="envy-card-icon">
            <Layers3 size={20} />
          </span>
          <Badge phase={composition.phase} />
        </div>
        <h2>
          <button onClick={() => onInspect(composition)}>
            {composition.name}
          </button>
        </h2>
        <p className="envy-card-context">
          {composition.project} / {composition.baseline}
          <span>Revision {composition.generation}</span>
        </p>
      </div>
      <div className="envy-card-services">
        <span className="envy-eyebrow">Changed services</span>
        <div className="envy-service-chips">
          {services.length ? (
            services.map((id) => (
              <span
                key={id}
                title={
                  composition.overrides[id].image ||
                  composition.overrides[id].build_id
                }
              >
                <Box size={12} />
                {id}
              </span>
            ))
          ) : (
            <span>
              <Layers3 size={12} />
              Complete baseline inheritance
            </span>
          )}
        </div>
      </div>
      <div className="envy-card-verification">
        <ShieldCheck size={15} />
        <span>
          {inactive
            ? composition.phase === "destroyed"
              ? "Preview removed · endpoint closed"
              : "Preview removal in progress"
            : !composition.endpoints.public.ready
              ? composition.phase === "failed"
                ? "Deployment failed · view logs"
                : "Waiting for endpoint readiness"
              : composition.verification_level === "routing"
                ? "Request routing verified"
                : composition.verification_level === "reachability"
                  ? "HTTP reachable · routing unverified"
                  : "Endpoint ready · routing unverified"}
        </span>
      </div>
      <div className="envy-card-endpoint">
        <span title={composition.endpoints.public.url}>
          {composition.endpoints.public.url || "Endpoint pending"}
        </span>
        <button
          onClick={() => void copyUrl()}
          aria-label="Copy preview URL"
          disabled={!composition.endpoints.public.url}
        >
          {copied ? <Check size={14} /> : <Copy size={14} />}
        </button>
      </div>
      {copyError && (
        <p role="alert" className="text-xs text-destructive">
          {copyError}
        </p>
      )}
      <footer>
        <span className="envy-card-expiry">
          <Clock size={13} />
          {composition.phase === "destroyed"
            ? "Removed"
            : formatTimeRemaining(composition.expires_at)}
        </span>
        <div className="envy-card-actions">
          {composition.endpoints.public.ready && !inactive ? (
            <Button
              size="sm"
              variant="link"
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
          ) : (
            <Button
              size="sm"
              variant="link"
              onClick={() => onInspect(composition)}
            >
              View details
            </Button>
          )}
        </div>
      </footer>
    </article>
  );
}
