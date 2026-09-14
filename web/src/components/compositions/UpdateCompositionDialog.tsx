import { RevisionPicker, selectedOverride } from "./RevisionPicker";
import React, { useState, useEffect, useRef } from "react";
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
  const { updateComposition, components, baselines = [] } = useEnvyApi();
  const baseline = baselines.find(
    (item) =>
      item.project === composition?.project &&
      item.id === composition?.baseline,
  );
  const componentIds = Array.from(
    new Set([
      ...Object.keys(composition?.overrides || {}),
      ...components
        .filter(
          (component) =>
            component.project === composition?.project &&
            component.overridable &&
            Boolean(baseline?.components[component.id]),
        )
        .map((component) => component.id),
    ]),
  ).sort();
  const [images, setImages] = useState<Record<string, string>>({});
  const [expectedGeneration, setExpectedGeneration] = useState<number>(1);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [conflict, setConflict] = useState<Composition | null>(null);

  const previousSelection = useRef<{ id?: string; open: boolean }>({
    open: false,
  });
  useEffect(() => {
    const previous = previousSelection.current;
    previousSelection.current = { id: composition?.id, open };
    // Polling can replace the composition object while the user edits.
    if (previous.id === composition?.id && previous.open === open) return;
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
  }, [composition, open]);

  if (!composition) return null;

  const selectedIds = componentIds.filter((id) => Object.hasOwn(images, id));
  const requestedOverrides = Object.fromEntries(
    selectedIds.map((id) => [id, selectedOverride(images[id])]),
  );
  const describe = (overrides: Composition["overrides"]) =>
    Object.entries(overrides)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(
        ([id, override]) =>
          `${id}: ${override.build_id ? `build:${override.build_id.slice(0, 12)}…` : override.image || "unselected"}`,
      );

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (selectedIds.some((id) => !images[id]?.trim())) {
      setFormError("Select an image or a published build for every component");
      return;
    }

    setSubmitting(true);
    setFormError(null);

    try {
      await updateComposition(composition.id, {
        expected_generation: expectedGeneration,
        overrides: requestedOverrides,
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
                  <div className="space-y-2">
                    <div>
                      <p className="font-semibold">Current selection</p>
                      <div className="font-mono">
                        {describe(conflict.overrides).map((line) => (
                          <p key={line}>{line}</p>
                        ))}
                        {Object.keys(conflict.overrides).length === 0 && (
                          <p>complete baseline inheritance</p>
                        )}
                      </div>
                    </div>
                    <div>
                      <p className="font-semibold">Your draft</p>
                      <div className="font-mono">
                        {describe(requestedOverrides).map((line) => (
                          <p key={line}>{line}</p>
                        ))}
                        {selectedIds.length === 0 && (
                          <p>complete baseline inheritance</p>
                        )}
                      </div>
                    </div>
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      setExpectedGeneration(conflict.generation);
                      setConflict(null);
                      setFormError(null);
                    }}
                  >
                    Rebase my draft on generation {conflict.generation}
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
                <div className="flex items-center justify-between">
                  <p className="text-xs font-medium">
                    Workload overrides ({selectedIds.length}/3)
                  </p>
                  {selectedIds.length === 0 && (
                    <span className="text-xs text-primary">
                      Complete baseline inheritance
                    </span>
                  )}
                </div>
                {componentIds.map((id) => {
                  const checked = Object.hasOwn(images, id);
                  return (
                    <div
                      key={id}
                      className="rounded border border-border p-3 space-y-2"
                    >
                      <label className="flex items-center gap-2 text-xs font-mono">
                        <input
                          type="checkbox"
                          checked={checked}
                          disabled={!checked && selectedIds.length >= 3}
                          onChange={(event) =>
                            setImages((old) => {
                              const next = { ...old };
                              if (event.target.checked) next[id] = "";
                              else delete next[id];
                              return next;
                            })
                          }
                        />
                        {id}
                      </label>
                      {checked && (
                        <RevisionPicker
                          key={`${composition.id}/${id}`}
                          project={composition.project}
                          component={id}
                          profile={
                            components.find(
                              (item) =>
                                item.project === composition.project &&
                                item.id === id,
                            )?.profile
                          }
                          value={images[id] || ""}
                          onChange={(value) =>
                            setImages((old) => ({ ...old, [id]: value }))
                          }
                        />
                      )}
                    </div>
                  );
                })}
                <p className="text-xs text-muted-foreground">
                  This is the complete desired selection. Removing a component
                  restores its baseline route; clearing every selection keeps
                  the preview URL and inherits the complete baseline.
                </p>
              </div>

              <div className="rounded-lg border border-border bg-muted/40 p-3 text-xs space-y-2">
                <p className="font-medium text-foreground">Requested change</p>
                <div className="grid gap-2 sm:grid-cols-2">
                  <div>
                    <span className="text-muted-foreground">Current</span>
                    <div className="font-mono">
                      {describe(composition.overrides).map((line) => (
                        <p key={line}>{line}</p>
                      ))}
                      {Object.keys(composition.overrides).length === 0 && (
                        <p>complete baseline inheritance</p>
                      )}
                    </div>
                  </div>
                  <div>
                    <span className="text-muted-foreground">Requested</span>
                    <div className="font-mono">
                      {describe(requestedOverrides).map((line) => (
                        <p key={line}>{line}</p>
                      ))}
                      {selectedIds.length === 0 && (
                        <p>complete baseline inheritance</p>
                      )}
                    </div>
                  </div>
                </div>
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
