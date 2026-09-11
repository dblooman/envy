import { RevisionPicker, selectedOverride } from "./RevisionPicker";
import React, { useState, useEffect } from "react";
import { RefreshCw, AlertCircle, ArrowUpRight } from "lucide-react";
import {
  Dialog,
  DialogPortal,
  DialogBackdrop,
  DialogPopup,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "../ui/dialog";
import { Button } from "../ui/button";
import { Composition } from "../../types/api";
import { useEnvyApi } from "../../context/ApiContext";
import { apiClient } from "../../lib/api-client";

interface UpdateCompositionDialogProps {
  composition: Composition | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function UpdateCompositionDialog({
  composition,
  open,
  onOpenChange,
}: UpdateCompositionDialogProps) {
  const { updateComposition, components } = useEnvyApi();
  const componentIds = Object.keys(composition?.overrides || {}).sort();
  const [images, setImages] = useState<Record<string, string>>({});
  const [expectedGeneration, setExpectedGeneration] = useState<number>(1);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [conflict, setConflict] = useState<Composition | null>(null);

  useEffect(() => {
    if (composition) {
      setExpectedGeneration(composition.generation);
      setConflict(null);
      setImages(
        Object.fromEntries(
          Object.entries(composition.overrides).map(([id, o]) => [
            id,
            o.build_id ? `build:${o.build_id}` : o.image || "",
          ]),
        ),
      );
    }
  }, [composition?.id, open]);

  if (!composition) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (componentIds.some((id) => !images[id]?.trim())) {
      setFormError("Select an image or a published build for every component");
      return;
    }

    setSubmitting(true);
    setFormError(null);

    try {
      await updateComposition(composition.id, {
        expected_generation: expectedGeneration,
        overrides: Object.fromEntries(
          componentIds.map((id) => [id, selectedOverride(images[id])]),
        ),
      });
      onOpenChange(false);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to update image";
      setFormError(msg);
      if (msg.includes("[conflict]")) {
        try {
          setConflict(await apiClient.getComposition(composition.id));
        } catch {
          /* retain the original conflict */
        }
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogPortal>
        <DialogBackdrop />
        <DialogPopup className="max-w-2xl max-h-[90vh] overflow-y-auto">
          <form onSubmit={handleSubmit}>
            <DialogHeader>
              <div className="flex items-center gap-2">
                <RefreshCw className="h-5 w-5 text-primary" />
                <DialogTitle>Rolling Image Update</DialogTitle>
              </div>
              <DialogDescription>
                Update{" "}
                <span className="font-semibold text-foreground">
                  {composition.name}
                </span>{" "}
                while keeping its URL.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-4 py-4 text-sm">
              {formError && (
                <div className="p-3 rounded-lg bg-rose-50 border border-rose-200 text-rose-800 dark:bg-rose-950/40 dark:border-rose-900 dark:text-rose-300 text-xs flex items-center gap-2">
                  <AlertCircle className="h-4 w-4 shrink-0 text-rose-600 dark:text-rose-400" />
                  <span>{formError}</span>
                </div>
              )}
              {conflict && (
                <div className="space-y-2 rounded-lg border border-amber-300 bg-amber-50 p-3 text-xs text-amber-900 dark:bg-amber-950/30 dark:text-amber-200">
                  <strong>
                    Generation {conflict.generation} is now current.
                  </strong>
                  <p>
                    Another caller changed this composition after you opened the
                    form. Review its selected versions before replacing your
                    draft.
                  </p>
                  <div className="font-mono">
                    {Object.entries(conflict.overrides).map(([id, o]) => (
                      <p key={id}>
                        {id}: {o.build_id ? `build:${o.build_id}` : o.image}
                      </p>
                    ))}
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      setExpectedGeneration(conflict.generation);
                      setImages(
                        Object.fromEntries(
                          Object.entries(conflict.overrides).map(([id, o]) => [
                            id,
                            o.build_id ? `build:${o.build_id}` : o.image || "",
                          ]),
                        ),
                      );
                      setConflict(null);
                      setFormError(null);
                    }}
                  >
                    Use generation {conflict.generation}
                  </Button>
                </div>
              )}

              <div className="p-3 rounded-lg bg-muted/50 border border-border text-xs space-y-1">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Current Image:</span>
                  <span className="font-mono text-foreground font-medium">
                    {Object.values(composition.overrides)
                      .map((o) => o.image)
                      .join(", ") || "unknown"}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">
                    Current Generation:
                  </span>
                  <span className="font-mono text-foreground font-medium">
                    Gen {composition.generation}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Stable URL:</span>
                  <span
                    className="text-foreground font-mono truncate max-w-50"
                    title={composition.endpoints.public.url}
                  >
                    {composition.endpoints.public.url}
                  </span>
                </div>
              </div>

              <div className="space-y-3">
                {componentIds.map((id) => (
                  <RevisionPicker
                    key={`${composition.id}/${id}/${composition.generation}`}
                    project={composition.project}
                    component={id}
                    profile={
                      components.find(
                        (c) => c.project === composition.project && c.id === id,
                      )?.profile
                    }
                    value={images[id] || ""}
                    onChange={(value) =>
                      setImages((old) => ({ ...old, [id]: value }))
                    }
                  />
                ))}
                <p className="text-xs text-muted-foreground">
                  Only changed images roll out. The selected component set stays
                  fixed; the update is not an atomic cutover.
                </p>
              </div>

              <div className="rounded-lg border border-border bg-muted/40 p-3 text-xs">
                This edit is based on generation {expectedGeneration}. If
                another caller updates it first, Envy rejects this request so
                you can review the newer revision.
              </div>

              <p className="text-[11px] text-muted-foreground">
                The URL, ID, and expiry remain stable. Readiness remains false
                until the workloads and the baseline's configured ingress checks
                pass.
              </p>
            </div>

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => onOpenChange(false)}
                disabled={submitting}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                size="sm"
                disabled={submitting}
                className="gap-2 shadow-sm"
              >
                {submitting ? (
                  "Updating..."
                ) : (
                  <>
                    <ArrowUpRight className="h-4 w-4" />
                    Deploy Generation {expectedGeneration + 1}
                  </>
                )}
              </Button>
            </DialogFooter>
          </form>
        </DialogPopup>
      </DialogPortal>
    </Dialog>
  );
}
