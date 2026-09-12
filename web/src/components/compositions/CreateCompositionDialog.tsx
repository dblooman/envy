import { RevisionPicker, selectedOverride } from "./RevisionPicker";
import React, { useState } from "react";
import { Rocket, Sparkles, AlertCircle } from "lucide-react";
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
import { Input } from "../ui/input";
import { useEnvyApi } from "../../context/ApiContext";

interface CreateCompositionDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess?: (id: string) => void;
}

const PRESET_IMAGES = [
  {
    label: "service-b:v2",
    image: "envy/service-b:v2",
    desc: "Standard v2 feature build",
  },
  {
    label: "service-b:v3",
    image: "envy/service-b:v3",
    desc: "Updated v3 release build",
  },
];

export function CreateCompositionDialog({
  open,
  onOpenChange,
  onSuccess,
}: CreateCompositionDialogProps) {
  const { createComposition, projects, baselines, components, isDemoMode } =
    useEnvyApi();
  const [name, setName] = useState("");
  const [images, setImages] = useState<Record<string, string>>({});
  const [ttl, setTtl] = useState("8h");
  const [idempotencyKey, setIdempotencyKey] = useState(
    () => globalThis.crypto?.randomUUID?.() || `web-${Date.now()}`,
  );
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const [projectId, setProjectId] = useState("demo");
  const [baselineId, setBaselineId] = useState("staging");
  const project = projects.find((p) => p.id === projectId) || projects[0];
  const choices = baselines.filter((b) => b.project === project?.id);
  const baseline = choices.find((b) => b.id === baselineId) || choices[0];
  const approved = components.filter(
    (c) =>
      c.project === project?.id && c.overridable && baseline?.components[c.id],
  );
  const selectionScope = `${project?.id}/${baseline?.id}/${approved.map((c) => c.id).join(",")}`;
  React.useEffect(() => {
    const first = approved.find((c) => c.id === "service-b") || approved[0];
    setImages(
      first
        ? {
            [first.id]:
              isDemoMode && project?.id === "demo" && first.id === "service-b"
                ? "envy/service-b:v2"
                : "",
          }
        : {},
    );
  }, [selectionScope, isDemoMode]);
  const selected = approved.filter((c) => Object.hasOwn(images, c.id));

  const generateRandomName = () => {
    const adjectives = [
      "swift",
      "stellar",
      "agile",
      "bright",
      "turbo",
      "crisp",
      "silent",
    ];
    const nouns = ["preview", "staging", "slice", "canary", "feature", "patch"];
    const randAdj = adjectives[Math.floor(Math.random() * adjectives.length)];
    const randNoun = nouns[Math.floor(Math.random() * nouns.length)];
    const num = Math.floor(Math.random() * 900) + 100;
    setName(`${randAdj}-${randNoun}-${num}`);
  };

  React.useEffect(() => {
    if (open && !name) {
      generateRandomName();
    }
  }, [open, name]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!project || !baseline || selected.length > 3) {
	  setFormError("Select a registered baseline and no more than three approved components");
      return;
    }
    if (!name.trim()) {
      setFormError("Composition name is required");
      return;
    }
    if (selected.some((c) => !images[c.id].trim())) {
      setFormError("Select an image or a published build for every component");
      return;
    }

    setSubmitting(true);
    setFormError(null);

    try {
      const comp = await createComposition(
        {
          project: project.id,
          baseline: baseline.id,
          name: name.trim(),
          overrides: Object.fromEntries(
            selected.map((c) => [c.id, selectedOverride(images[c.id])]),
          ),
          ttl: ttl || "8h",
        },
        idempotencyKey,
      );

      onOpenChange(false);
      setName("");
      setIdempotencyKey(
        globalThis.crypto?.randomUUID?.() || `web-${Date.now()}`,
      );
      if (onSuccess) {
        onSuccess(comp.id);
      }
    } catch (err: unknown) {
      const msg =
        err instanceof Error ? err.message : "Failed to create composition";
      setFormError(msg);
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
                <Rocket className="h-5 w-5 text-primary" />
                <DialogTitle>Create Preview Composition</DialogTitle>
              </div>
              <DialogDescription>
                Combine shared staging with selected workload overrides.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-4 py-4 text-sm">
              {formError && (
                <div className="p-3 rounded-lg bg-rose-50 border border-rose-200 text-rose-800 dark:bg-rose-950/40 dark:border-rose-900 dark:text-rose-300 text-xs flex items-center gap-2">
                  <AlertCircle className="h-4 w-4 shrink-0 text-rose-600 dark:text-rose-400" />
                  <span>{formError}</span>
                </div>
              )}

              {/* Name */}
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <label className="text-xs font-medium text-foreground">
                    Composition Name
                  </label>
                  <button
                    type="button"
                    onClick={generateRandomName}
                    className="text-[11px] text-primary hover:underline flex items-center gap-1 cursor-pointer"
                  >
                    <Sparkles className="h-3 w-3" /> Randomize
                  </button>
                </div>
                <Input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. pr-42-pricing-service"
                  required
                />
              </div>

              {/* Project & Baseline */}
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-foreground">
                    Project
                  </label>
                  <select
                    aria-label="Project"
                    value={project?.id || ""}
                    onChange={(e) => {
                      setProjectId(e.target.value);
                      setBaselineId("");
                      setImages({});
                    }}
                    className="w-full rounded border border-input bg-card p-2"
                  >
                    {projects.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-foreground">
                    Baseline
                  </label>
                  <select
                    aria-label="Baseline"
                    value={baseline?.id || ""}
                    onChange={(e) => {
                      setBaselineId(e.target.value);
                      setImages({});
                    }}
                    className="w-full rounded border border-input bg-card p-2"
                  >
                    {choices.map((b) => (
                      <option key={b.id} value={b.id}>
                        {b.id}
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              {baseline?.verification?.kind === "http" && (
                <p className="text-xs text-amber-700 dark:text-amber-300">
                  This baseline uses HTTP reachability checks. Run your
                  application checks to verify context propagation and override
                  selection.
                </p>
              )}
              <fieldset className="space-y-3 border-t border-border pt-3">
                <legend className="text-xs font-medium">
                  Workload overrides ({selected.length}/3)
                </legend>
                {approved.length === 0 && (
                  <p className="text-xs text-muted-foreground">
                    This baseline has no approved override components.
                  </p>
                )}
                {approved.map((c) => {
                  const checked = Object.hasOwn(images, c.id);
                  return (
                    <div
                      key={c.id}
                      className="rounded border border-border p-3 space-y-2"
                    >
                      <label className="flex items-center gap-2 text-xs font-mono">
                        <input
                          type="checkbox"
                          checked={checked}
                          disabled={!checked && selected.length >= 3}
                          onChange={(e) =>
                            setImages((old) => {
                              const next = { ...old };
                              if (e.target.checked) next[c.id] = "";
                              else delete next[c.id];
                              return next;
                            })
                          }
                        />
                        {c.id}
                      </label>
                      {checked && (
                        <>
                          <RevisionPicker
                            key={`${selectionScope}/${c.id}`}
                            project={project!.id}
                            component={c.id}
                            profile={c.profile}
                            value={images[c.id]}
                            onChange={(value) =>
                              setImages((old) => ({ ...old, [c.id]: value }))
                            }
                          />
                          {isDemoMode &&
                            project?.id === "demo" &&
                            c.id === "service-b" && (
                              <div className="flex gap-2">
                                {PRESET_IMAGES.map((p) => (
                                  <button
                                    key={p.image}
                                    type="button"
                                    onClick={() =>
                                      setImages((old) => ({
                                        ...old,
                                        [c.id]: p.image,
                                      }))
                                    }
                                    className="rounded border border-border px-2 py-1 text-xs hover:bg-muted"
                                  >
                                    {p.label}
                                  </button>
                                ))}
                              </div>
                            )}
                        </>
                      )}
                    </div>
                  );
                })}
                <p className="text-xs text-muted-foreground">
                  Each selected component uses its approved profile. All other
                  components remain inherited. You can select none to create a
                  stable preview URL that inherits the complete baseline.
                </p>
              </fieldset>

              {/* TTL and Idempotency */}
              <div className="grid grid-cols-1 gap-3 pt-1 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-foreground">
                    TTL (Lifetime)
                  </label>
                  <select
                    value={ttl}
                    onChange={(e) => setTtl(e.target.value)}
                    className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  >
                    <option value="1h" className="bg-card text-foreground">
                      1 hour
                    </option>
                    <option value="4h" className="bg-card text-foreground">
                      4 hours
                    </option>
                    <option value="8h" className="bg-card text-foreground">
                      8 hours (default)
                    </option>
                    <option value="24h" className="bg-card text-foreground">
                      24 hours (max)
                    </option>
                  </select>
                </div>

                <div className="rounded-lg border border-border bg-muted/40 p-3 text-xs text-muted-foreground">
                  Retries of this form reuse one request identity, preventing
                  duplicate compositions.
                </div>
              </div>
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
                disabled={submitting || selected.length === 0}
                className="gap-2 shadow-sm"
              >
                {submitting ? (
                  <>
                    <span className="h-3.5 w-3.5 rounded-full border-2 border-white/30 border-t-white animate-spin" />
                    Deploying...
                  </>
                ) : (
                  <>
                    <Rocket className="h-3.5 w-3.5" />
                    Launch Preview
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
