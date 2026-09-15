import { RevisionPicker, selectedOverride } from "./RevisionPicker";
import React, { useEffect, useRef, useState } from "react";
import {
  ArrowRight,
  Check,
  Layers3,
  Rocket,
  Sparkles,
  AlertCircle,
  SlidersHorizontal,
} from "lucide-react";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { useEnvyApi } from "../../context/ApiContext";
import { durationSeconds, lifetimeOptions } from "../../lib/duration";
interface CreateCompositionViewProps {
  open: boolean;
  onCancel: () => void;
  onSuccess: (id: string) => void;
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

export function CreateCompositionView({
  open,
  onCancel,
  onSuccess,
}: CreateCompositionViewProps) {
  const {
    createComposition,
    projects,
    baselines,
    components,
    isDemoMode,
    installation,
  } = useEnvyApi();
  const [name, setName] = useState("");
  const [step, setStep] = useState(0);
  const stepHeading = useRef<HTMLHeadingElement>(null);
  const request = useRef<{ fingerprint: string; key: string } | null>(null);
  const busy = useRef(false);
  useEffect(() => {
    if (open) stepHeading.current?.focus();
  }, [step, open]);
  const [images, setImages] = useState<Record<string, string>>({});
  const [messageIsolation, setMessageIsolation] = useState(false);
  const [ttl, setTtl] = useState("");
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
  const selectionScope = `${isDemoMode}/${project?.id}/${baseline?.id}`;
  const initializedScope = useRef("");
  const firstId = (approved.find((c) => c.id === "service-b") || approved[0])
    ?.id;
  React.useEffect(() => {
    if (initializedScope.current === selectionScope) return;
    initializedScope.current = selectionScope;
    setImages(
      firstId
        ? {
            [firstId]:
              isDemoMode && project?.id === "demo" && firstId === "service-b"
                ? "envy/service-b:v2"
                : "",
          }
        : {},
    );
  }, [selectionScope, isDemoMode, firstId, project?.id]);
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

  const effectiveTtl = ttl || installation?.default_ttl;
  const validate = (target: number) => {
    if (!project || !baseline)
      return "Select a registered project and baseline.";
    if (!name.trim()) return "Preview name is required.";
    if (target >= 1 && selected.length > 3)
      return "Select no more than three approved components.";
    if (target >= 1 && selected.some((c) => !images[c.id]?.trim()))
      return "Select an image or a published build for every component.";
    if (effectiveTtl) {
      const seconds = durationSeconds(effectiveTtl);
      const maximum = installation?.max_ttl
        ? durationSeconds(installation.max_ttl)
        : null;
      if (!seconds || (maximum !== null && seconds > maximum))
        return "Choose a lifetime within this installation’s maximum.";
    }
    return null;
  };
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (busy.current) return;
    const problem = validate(step);
    if (problem) {
      setFormError(problem);
      return;
    }
    setFormError(null);
    if (step < 2) {
      setStep(step + 1);
      return;
    }
    const payload = {
      message_isolation: messageIsolation,
      project: project!.id,
      baseline: baseline!.id,
      name: name.trim(),
      overrides: Object.fromEntries(
        selected.map((c) => [c.id, selectedOverride(images[c.id])]),
      ),
      ...(ttl ? { ttl } : {}),
    };
    const fingerprint = JSON.stringify(payload);
    if (request.current?.fingerprint !== fingerprint)
      request.current = {
        fingerprint,
        key: globalThis.crypto?.randomUUID?.() || `web-${Date.now()}`,
      };
    busy.current = true;
    setSubmitting(true);
    try {
      const comp = await createComposition(payload, request.current.key);
      setName("");
      setStep(0);
      setTtl("");
      request.current = null;
      setImages(
        firstId
          ? {
              [firstId]:
                isDemoMode && firstId === "service-b"
                  ? "envy/service-b:v2"
                  : "",
            }
          : {},
      );
      onSuccess(comp.id);
    } catch (err) {
      setFormError(
        err instanceof Error ? err.message : "Failed to create preview",
      );
    } finally {
      busy.current = false;
      setSubmitting(false);
    }
  };
  if (!open) return null;
  return (
    <div className="envy-create-layout">
      <div className="min-w-0">
        <ol className="envy-steps" aria-label="Create preview progress">
          {["Choose baseline", "Select changes", "Review & create"].map(
            (label, index) => (
              <li
                key={label}
                aria-current={step === index ? "step" : undefined}
              >
                <span>{step > index ? <Check size={15} /> : index + 1}</span>
                {label}
              </li>
            ),
          )}
        </ol>
        <form
          className="envy-panel envy-create-form"
          onSubmit={handleSubmit}
          noValidate
        >
          <div className="envy-section-heading">
            <span className="envy-eyebrow">Step {step + 1} of 3</span>
            <h2 ref={stepHeading} tabIndex={-1}>
              {
                [
                  "Choose your starting point",
                  "What are you changing?",
                  "Ready when you are.",
                ][step]
              }
            </h2>
            <p>
              {
                [
                  "Everything you don’t override stays on this shared baseline.",
                  "Choose up to three approved components, or inherit the complete baseline.",
                  "Review your configuration before deploying the preview.",
                ][step]
              }
            </p>
          </div>
          {formError && (
            <div role="alert" className="envy-error-banner">
              <AlertCircle size={17} />
              {formError}
            </div>
          )}
          <fieldset disabled={submitting} className="min-w-0 space-y-5">
            {step === 0 && (
              <div className="space-y-5">
                {" "}
                {/* Name */}
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between">
                    <label
                      htmlFor="composition-name"
                      className="text-xs font-medium text-foreground"
                    >
                      Preview name
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
                    id="composition-name"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="e.g. pr-42-pricing-service"
                    aria-required="true"
                  />
                </div>
                {/* Project & Baseline */}
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <div className="space-y-1.5">
                    <label
                      htmlFor="composition-project"
                      className="text-xs font-medium text-foreground"
                    >
                      Project
                    </label>
                    <select
                      id="composition-project"
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
                    <label
                      htmlFor="composition-baseline"
                      className="text-xs font-medium text-foreground"
                    >
                      Baseline
                    </label>
                    <select
                      id="composition-baseline"
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
                    application checks to verify context propagation and
                    override selection.
                  </p>
                )}
              </div>
            )}
            {step === 1 && (
              <div>
                {" "}
                <fieldset className="space-y-3 border-t border-border pt-3">
                  <legend className="text-xs font-medium">
                    Service overrides ({selected.length}/3)
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
                        className={`envy-override-option ${checked ? "is-selected" : ""}`}
                      >
                        <label className="flex items-center gap-3 text-sm font-medium">
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
              </div>
            )}
            {step === 2 && (
              <div className="space-y-5">
                <div className="envy-review-banner">
                  <Layers3 size={24} />
                  <div>
                    <h3>{name}</h3>
                    <p>
                      {selected.length
                        ? `${selected.length} service ${selected.length === 1 ? "override" : "overrides"}`
                        : "Complete baseline inheritance"}{" "}
                      on {baseline?.id}
                    </p>
                  </div>
                </div>
                <dl className="envy-facts">
                  {selected.map((c) => (
                    <div key={c.id}>
                      <dt>{c.id}</dt>
                      <dd className="font-mono">
                        {images[c.id].startsWith("build:")
                          ? `Published build: ${images[c.id].slice(6)}`
                          : images[c.id]}
                      </dd>
                    </div>
                  ))}
                </dl>
                <p className="text-sm text-muted-foreground">
                  Unselected components remain on the shared baseline. Workload
                  readiness, HTTP reachability, and request routing are reported
                  separately after creation.
                </p>
              </div>
            )}
            <label className="flex items-start gap-3 text-sm">
              <input
                type="checkbox"
                checked={messageIsolation}
                onChange={(event) => setMessageIsolation(event.target.checked)}
              />
              <span>
                Isolate Pub/Sub messages
                <span className="block text-muted-foreground">
                  Keep messages out of baseline consumers. Inspect them or
                  attach a worker later. Requires registered messaging
                  integration; fixed for this preview.
                </span>
              </span>
            </label>
            <details className="envy-advanced">
              <summary>
                <SlidersHorizontal size={16} />
                Advanced configuration
              </summary>
              <div className="mt-4 space-y-3">
                <label
                  htmlFor="composition-ttl"
                  className="text-sm font-medium"
                >
                  Lifetime
                </label>
                <select
                  id="composition-ttl"
                  value={ttl}
                  onChange={(e) => setTtl(e.target.value)}
                  className="envy-select"
                >
                  <option value="">
                    Installation default
                    {installation?.default_ttl
                      ? ` (${installation.default_ttl})`
                      : ""}
                  </option>
                  {lifetimeOptions(installation?.max_ttl).map((value) => (
                    <option key={value} value={value}>
                      {value}
                    </option>
                  ))}
                </select>
                <p className="text-xs text-muted-foreground">
                  {installation?.max_ttl
                    ? `Maximum lifetime: ${installation.max_ttl}.`
                    : "The server applies its configured lifetime limits."}
                </p>
              </div>
            </details>
          </fieldset>
          <div className="envy-form-actions">
            <div className="flex gap-2">
              <Button
                type="button"
                variant="ghost"
                onClick={onCancel}
                disabled={submitting}
              >
                Close draft
              </Button>
              {step > 0 && (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    setStep(step - 1);
                    setFormError(null);
                  }}
                  disabled={submitting}
                >
                  Back
                </Button>
              )}
            </div>
            <Button type="submit" disabled={submitting}>
              {step < 2 ? (
                <>
                  Continue
                  <ArrowRight />
                </>
              ) : (
                <>
                  <Rocket />
                  {submitting ? "Creating preview…" : "Create preview"}
                </>
              )}
            </Button>
          </div>
          <p className="mt-4 text-xs text-muted-foreground">
            Your draft stays in this browser tab while you navigate. Reloading
            clears it.
          </p>
        </form>
      </div>
      <aside className="envy-configuration-summary">
        <span className="envy-eyebrow">Your configuration</span>
        <h2>
          A small change.
          <br />A complete environment.
        </h2>
        <p>Use what’s already running. Replace only what you need.</p>
        <dl className="envy-facts">
          <div>
            <dt>Preview</dt>
            <dd>{name || "Untitled preview"}</dd>
          </div>
          <div>
            <dt>Project</dt>
            <dd>{project?.name || "None registered"}</dd>
          </div>
          <div>
            <dt>Baseline</dt>
            <dd>{baseline?.id || "None registered"}</dd>
          </div>
          <div>
            <dt>Lifetime</dt>
            <dd>{effectiveTtl || "Installation default"}</dd>
          </div>
        </dl>
        <div className="envy-summary-services">
          <span className="envy-eyebrow">Overrides · {selected.length}</span>
          {selected.map((c) => (
            <div key={c.id}>
              <strong>{c.id}</strong>
              <code>{images[c.id] || "Choose a revision"}</code>
            </div>
          ))}
          {!selected.length && <p>Complete baseline inheritance</p>}
        </div>
        <p className="flex items-center gap-2">
          <Layers3 size={17} />
          All other components stay shared.
        </p>
      </aside>
    </div>
  );
}
