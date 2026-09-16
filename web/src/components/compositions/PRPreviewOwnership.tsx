import { useEffect, useState } from "react";
import { apiClient } from "../../lib/api-client";
import type { PRPreview } from "../../types/github";

export function PRPreviewOwnership({
  id,
  compositionId,
}: {
  id: string;
  compositionId: string;
}) {
  const [preview, setPreview] = useState<PRPreview>();
  const [error, setError] = useState("");
  useEffect(() => {
    let cancelled = false;
    async function refresh() {
      try {
        const result = await apiClient.prPreview(id);
        if (!cancelled) {
          setPreview(result);
          setError("");
        }
      } catch (e) {
        if (!cancelled)
          setError(
            e instanceof Error ? e.message : "Unable to load PR ownership",
          );
      }
    }
    void refresh();
    const timer = setInterval(() => void refresh(), 15000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [id]);
  return (
    <div className="text-sm space-y-1">
      <p>
        Managed by GitHub PR preview {id}. Use Catalog → GitHub to restart or
        stop automatic updates.
      </p>
      {error && <p role="alert">{error}</p>}
      {preview && (
        <>
          <a href={preview.pr_url} target="_blank" rel="noreferrer">
            Pull request #{preview.number}
          </a>
          {preview.composition_id === compositionId ? (
            <>
              <p>
                {preview.status} · {preview.reason}
              </p>
              <p className="font-mono break-all">
                Requested: {preview.requested_sha}
                <br />
                Deployed: {preview.deployed_sha || "None"}
              </p>
              <p>Expires: {preview.expires_at}</p>
            </>
          ) : (
            <p>This composition belongs to a retired preview lifecycle.</p>
          )}
        </>
      )}
    </div>
  );
}
