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
  const { createComposition, projects, baselines, components } = useEnvyApi();
  const [name, setName] = useState("");
  const [overrideImage, setOverrideImage] = useState("envy/service-b:v2");
  const [ttl, setTtl] = useState("8h");
  const [idempotencyKey, setIdempotencyKey] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const [projectId, setProjectId] = useState("demo");
  const [baselineId, setBaselineId] = useState("staging");
  const [componentId, setComponentId] = useState("service-b");
  const project = projects.find(p => p.id === projectId) || projects[0];
  const choices = baselines.filter(b => b.project === project?.id);
  const baseline = choices.find(b => b.id === baselineId) || choices[0];
  const approved = components.filter(c => c.project === project?.id && c.overridable && baseline?.components[c.id]);
  const component = approved.find(c => c.id === componentId) || approved[0];

  const handlePresetClick = (img: string) => {
    setOverrideImage(img);
  };

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
    if (!project || !baseline || !component) { setFormError("Select a registered baseline and approved component"); return; }
    if (!name.trim()) {
      setFormError("Composition name is required");
      return;
    }
    if (!overrideImage.trim()) {
      setFormError("Override image is required");
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
          overrides: {
            [component.id]: {
              image: overrideImage.trim(),
            },
          },
          ttl: ttl || "8h",
        },
        idempotencyKey.trim() || undefined,
      );

      onOpenChange(false);
      setName("");
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
        <DialogPopup className="max-w-lg">
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
                  <select aria-label="Project" value={project?.id || ""} onChange={e => { setProjectId(e.target.value); setBaselineId(""); setComponentId(""); setOverrideImage(""); }} className="w-full rounded border border-input bg-card p-2">
                    {projects.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                  </select>
                </div>
                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-foreground">
                    Baseline
                  </label>
                  <select aria-label="Baseline" value={baseline?.id || ""} onChange={e => { setBaselineId(e.target.value); setComponentId(""); setOverrideImage(""); }} className="w-full rounded border border-input bg-card p-2">
                    {choices.map(b => <option key={b.id} value={b.id}>{b.id}</option>)}
                  </select>
                </div>
              </div>

              {/* Workload Overrides */}
              <div className="space-y-2 pt-1">
                <div className="flex items-center justify-between">
                  <label className="text-xs font-medium text-foreground">
                    Override Component:{" "}
                    <select aria-label="Override component" value={component?.id || ""} onChange={e => {setComponentId(e.target.value); setOverrideImage("");}} className="rounded border border-input bg-card p-2">
                    {approved.map(c => <option key={c.id} value={c.id}>{c.id}</option>)}
                    </select>
                  </label>
                </div>

                <div className="grid grid-cols-2 gap-2 mb-2">
                  {(component?.id === "service-b" ? PRESET_IMAGES : []).map((preset) => (
                    <button
                      key={preset.image}
                      type="button"
                      onClick={() => handlePresetClick(preset.image)}
                      className={`p-2.5 rounded-lg border text-left text-xs transition-all cursor-pointer ${
                        overrideImage === preset.image
                          ? 'border-zinc-900 bg-zinc-100 text-zinc-950 ring-1 ring-zinc-900 dark:border-zinc-100 dark:bg-zinc-800 dark:text-zinc-50 dark:ring-zinc-100 shadow-2xs'
                          : 'border-border bg-card text-muted-foreground hover:border-zinc-400 dark:hover:border-zinc-600'
                      }`}
                    >
                      <div className="font-semibold text-foreground">
                        {preset.label}
                      </div>
                      <div className="text-[10px] text-muted-foreground truncate">
                        {preset.desc}
                      </div>
                    </button>
                  ))}
                </div>

                <div className="space-y-1">
                  <label className="text-[11px] text-muted-foreground">
                    Custom Image Tag
                  </label>
                  <Input
                    value={overrideImage}
                    onChange={(e) => setOverrideImage(e.target.value)}
                    placeholder="envy/service-b:v2"
                    required
                  />
                </div>
              </div>

              {/* TTL and Idempotency */}
              <div className="grid grid-cols-2 gap-3 pt-1">
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

                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-foreground">
                    Idempotency Key (Optional)
                  </label>
                  <Input
                    value={idempotencyKey}
                    onChange={(e) => setIdempotencyKey(e.target.value)}
                    placeholder="e.g. pr-123-ci-run"
                  />
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
                disabled={submitting || !component}
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
