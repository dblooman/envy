import { isDeploymentProfile } from "../../lib/preview-profile";
import { useEffect, useRef, useState } from "react";
import { useEnvyApi } from "../../context/ApiContext";
import { ApiRequestError, apiClient } from "../../lib/api-client";
import type {
  Baseline,
  CatalogManifest,
  CatalogReport,
  Component,
  OnboardingDraft,
} from "../../types/api";
import { Button } from "../ui/button";
import { TextField, KeyValues, entryMap, type Entry } from "./OnboardingFields";
import { PreviewPreparation } from "./PreviewPreparation";

export interface OnboardingTarget {
  project: string;
  baseline: string;
}
interface ComponentDraft {
  id: string;
  service: string;
  port: string;
  image: string;
  overridable: boolean;
  repository: string;
  profile: "deployment" | "deployment-composite" | "http-small";
  health: string;
  readiness: string;
  env: Entry[];
  secrets: string;
}
const newComponent = (): ComponentDraft => ({
  id: "",
  service: "",
  port: "8080",
  image: "",
  overridable: true,
  repository: "",
  profile: "deployment",
  health: "/healthz",
  readiness: "/readyz",
  env: [],
  secrets: "",
});

export function ApplicationOnboarding({
  resume,
  onCreate,
  onClose,
}: {
  resume?: OnboardingTarget;
  onCreate: (target: OnboardingTarget) => void;
  onClose: () => void;
}) {
  const { projects, components, baselines, refreshAll } = useEnvyApi();
  const existing = resume
    ? baselines.find(
        (b) => b.project === resume.project && b.id === resume.baseline,
      )
    : undefined;
  const [saved, setSaved] = useState<Baseline | undefined>(existing);
  const [savedComponents, setSavedComponents] = useState<Component[]>(
    existing
      ? components.filter(
          (c) =>
            c.project === existing.project &&
            Object.hasOwn(existing.components, c.id),
        )
      : [],
  );
  const [stage, setStage] = useState(existing ? 2 : 0);
  const [projectMode, setProjectMode] = useState("");
  const [projectId, setProjectId] = useState("");
  const [projectName, setProjectName] = useState("");
  const [baselineId, setBaselineId] = useState("staging");
  const [revision, setRevision] = useState("v1");
  const [endpoint, setEndpoint] = useState("");
  const [namespace, setNamespace] = useState("");
  const [gateway, setGateway] = useState("");
  const [gatewayNamespace, setGatewayNamespace] = useState("");
  const [listener, setListener] = useState("");
  const [entry, setEntry] = useState("");
  const [verification, setVerification] = useState<"http" | "envy-chain">(
    "http",
  );
  const [path, setPath] = useState("/");
  const [status, setStatus] = useState("200");
  const [chain, setChain] = useState("");
  const [rows, setRows] = useState<ComponentDraft[]>([newComponent()]);
  const [report, setReport] = useState<CatalogReport>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [draftRevision, setDraftRevision] = useState(0);
  const [draftMessage, setDraftMessage] = useState("");
  const pending = useRef(false);
  const alive = useRef(false);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    heading.current?.focus();
  }, [stage]);
  const invalidate = () => {
    setReport(undefined);
    setError("");
  };
  const field = (
    label: string,
    value: string,
    setter: (value: string) => void,
    type = "text",
  ) => (
    <TextField
      label={label}
      value={value}
      type={type}
      onChange={(next) => {
        invalidate();
        setter(next);
      }}
    />
  );
  const update = (index: number, patch: Partial<ComponentDraft>) => {
    invalidate();
    setRows(rows.map((old, i) => (i === index ? { ...old, ...patch } : old)));
  };
  const manifest = (strict = true): CatalogManifest => {
    const project =
      projectMode === ""
        ? { id: projectId.trim(), name: projectName.trim() }
        : projects.find((p) => p.id === projectMode);
    if (!project) throw new Error("Select a project.");
    if (strict && new Set(rows.map((r) => r.id.trim())).size !== rows.length)
      throw new Error("Component IDs must be unique.");
    return {
      api_version: "envy/v1",
      project,
      components: rows.map((row) => ({
        id: row.id.trim(),
        project: project.id,
        protocol: "http",
        port: Number(row.port),
        profile: row.profile,
        overridable: row.overridable,
        ...(row.repository.trim() ? { repository: row.repository.trim() } : {}),
        ...(row.profile === "http-small"
          ? {
              health_path: row.health,
              readiness_path: row.readiness,
              env: entryMap(row.env),
              image_pull_secrets: row.secrets
                .split(",")
                .map((name) => name.trim())
                .filter(Boolean),
            }
          : {}),
      })),
      baseline: {
        project: project.id,
        id: baselineId.trim(),
        revision: revision.trim(),
        endpoint: endpoint.trim(),
        routing: {
          namespace: namespace.trim(),
          gateway: gateway.trim(),
          entry_component: entry.trim(),
          ...(gatewayNamespace.trim()
            ? { gateway_namespace: gatewayNamespace.trim() }
            : {}),
          ...(listener.trim() ? { gateway_section_name: listener.trim() } : {}),
        },
        verification:
          verification === "http"
            ? { kind: "http", path, expected_status: Number(status) }
            : {
                kind: "envy-chain",
                chain: chain.split(",").map((id) => id.trim()),
              },
        components: Object.fromEntries(
          rows.map((row) => [
            row.id.trim(),
            {
              service_host: `${row.service.trim()}.${namespace.trim()}.svc.cluster.local`,
              port: Number(row.port),
              image: row.image.trim(),
            },
          ]),
        ),
      },
    };
  };
  const draftProject = projectMode || projectId.trim();
  const saveDraft = async () => {
    setDraftMessage("");
    try {
      if (!/^[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(draftProject))
        throw new Error("Enter a valid project ID before saving.");
      const configuration = manifest(false);
      const safe: CatalogManifest = {
        ...configuration,
        components: configuration.components.map((component) => {
          const safeComponent = { ...component };
          delete safeComponent.env;
          return safeComponent;
        }),
      };
      const savedDraft = await apiClient.saveOnboardingDraft({
        project: draftProject,
        revision: draftRevision,
        stage: Math.min(stage, 1),
        configuration: safe,
      });
      if (!alive.current) return;
      setDraftRevision(savedDraft.revision);
      setDraftMessage(
        `Preparation saved at revision ${savedDraft.revision}. Environment values are not saved.`,
      );
    } catch (err) {
      if (alive.current)
        setError(
          err instanceof ApiRequestError && err.status === 404
            ? "Preparation drafts are unavailable on this installation."
            : err instanceof Error
              ? err.message
              : String(err),
        );
    }
  };
  const loadDraft = async () => {
    setDraftMessage("");
    try {
      const draft: OnboardingDraft =
        await apiClient.getOnboardingDraft(draftProject);
      if (!alive.current) return;
      const m = draft.configuration;
      setDraftRevision(draft.revision);
      setProjectMode(
        projects.some((p) => p.id === draft.project) ? draft.project : "",
      );
      setProjectId(draft.project);
      setProjectName(m.project.name || "");
      setBaselineId(m.baseline.id || "staging");
      setRevision(m.baseline.revision || "v1");
      setEndpoint(m.baseline.endpoint || "");
      setNamespace(m.baseline.routing?.namespace || "");
      setGateway(m.baseline.routing?.gateway || "");
      setGatewayNamespace(m.baseline.routing?.gateway_namespace || "");
      setListener(m.baseline.routing?.gateway_section_name || "");
      setEntry(m.baseline.routing?.entry_component || "");
      setVerification(
        m.baseline.verification?.kind === "envy-chain" ? "envy-chain" : "http",
      );
      setPath(m.baseline.verification?.path || "/");
      setStatus(String(m.baseline.verification?.expected_status || 200));
      setChain(m.baseline.verification?.chain?.join(",") || "");
      setRows(
        m.components?.length
          ? m.components.map((c) => ({
              ...newComponent(),
              id: c.id,
              service:
                m.baseline.components?.[c.id]?.service_host?.split(".")[0] ||
                "",
              port: String(c.port || 8080),
              image: m.baseline.components?.[c.id]?.image || "",
              overridable: c.overridable ?? true,
              repository: c.repository || "",
              profile: c.profile as ComponentDraft["profile"],
              health: c.health_path || "/healthz",
              readiness: c.readiness_path || "/readyz",
              secrets: c.image_pull_secrets?.join(",") || "",
            }))
          : [newComponent()],
      );
      setStage(Math.min(draft.stage, 1));
      setReport(undefined);
      setDraftMessage(
        `Resumed preparation revision ${draft.revision}. Review all fields before registration.`,
      );
    } catch (err) {
      if (alive.current)
        setError(
          err instanceof ApiRequestError && err.status === 404
            ? "No saved preparation was found, or this installation does not support drafts."
            : err instanceof Error
              ? err.message
              : String(err),
        );
    }
  };
  const discardDraft = async () => {
    try {
      if (draftRevision > 0)
        await apiClient.deleteOnboardingDraft(draftProject, draftRevision);
      if (!alive.current) return;
      setDraftRevision(0);
      setDraftMessage(
        "Saved preparation discarded. No preview resources were created.",
      );
    } catch (err) {
      if (alive.current)
        setError(err instanceof Error ? err.message : String(err));
    }
  };
  const submit = async (apply: boolean) => {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setError("");
    try {
      if (apply && !report)
        throw new Error("Validate this configuration before registration.");
      const result = apply
        ? await apiClient.applyCatalog(report!.configuration)
        : await apiClient.validateCatalog(manifest());
      if (!alive.current) return;
      setReport(result);
      if (apply) {
        setSaved(result.configuration.baseline);
        setSavedComponents(result.configuration.components);
        setStage(2);
        await refreshAll();
      }
    } catch (err) {
      if (alive.current) {
        setError(err instanceof Error ? err.message : String(err));
        if (!apply) setReport(undefined);
      }
    } finally {
      pending.current = false;
      if (alive.current) setBusy(false);
    }
  };
  return (
    <div className="space-y-5">
      <ol className="envy-steps" aria-label="Application onboarding progress">
        {[
          "Describe baseline",
          "Configure and register",
          "Prepare overrides",
          "Create first preview",
        ].map((label, index) => (
          <li key={label} aria-current={stage === index ? "step" : undefined}>
            <span>{index + 1}</span>
            {label}
          </li>
        ))}
      </ol>
      <h2 ref={heading} tabIndex={-1} className="text-xl font-semibold">
        {
          [
            "Describe your existing application",
            "Configure and register the baseline",
            "Prepare service overrides",
            "Create your first preview",
          ][stage]
        }
      </h2>
      {error && (
        <p role="alert" className="envy-error-banner">
          {error}
        </p>
      )}
      {stage < 2 && (
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={() => void saveDraft()}
          >
            Save preparation
          </Button>
          <Button
            type="button"
            variant="ghost"
            onClick={() => void loadDraft()}
            disabled={!draftProject}
          >
            Load saved preparation
          </Button>
          {draftRevision > 0 && (
            <Button
              type="button"
              variant="ghost"
              onClick={() => void discardDraft()}
            >
              Discard saved preparation
            </Button>
          )}
          {draftMessage && (
            <p role="status" className="text-sm">
              {draftMessage}
            </p>
          )}
        </div>
      )}
      {stage < 2 && (
        <form
          className="envy-panel space-y-5"
          onSubmit={(event) => {
            event.preventDefault();
            if (stage === 0) {
              setStage(1);
              setError("");
            } else void submit(false);
          }}
        >
          <fieldset disabled={busy} className="space-y-5">
            {stage === 0 && (
              <>
                <p>
                  Use existing Kubernetes Services and ingress. This flow does
                  not install infrastructure. Register the complete baseline
                  catalog, including services that stay inherited.
                </p>
                <label className="block space-y-1">
                  Project
                  <select
                    className="envy-select"
                    value={projectMode}
                    onChange={(event) => {
                      invalidate();
                      setProjectMode(event.target.value);
                      setDraftRevision(0);
                      setDraftMessage("");
                    }}
                  >
                    <option value="">New project</option>
                    {projects.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                  </select>
                </label>
                {projectMode === "" && (
                  <div className="grid gap-3 sm:grid-cols-2">
                    {field("Project ID", projectId, (value) => {
                      setProjectId(value);
                      setDraftRevision(0);
                      setDraftMessage("");
                    })}
                    {field("Project name", projectName, setProjectName)}
                  </div>
                )}
                <div className="grid gap-3 sm:grid-cols-2">
                  {field("Baseline ID", baselineId, setBaselineId)}
                  {field("Baseline revision", revision, setRevision)}
                  {field("Baseline endpoint", endpoint, setEndpoint)}
                  {field("Namespace", namespace, setNamespace)}
                  {field("Gateway", gateway, setGateway)}
                  {field(
                    "Gateway namespace (optional)",
                    gatewayNamespace,
                    setGatewayNamespace,
                  )}
                  {field("Gateway listener (optional)", listener, setListener)}
                  {field("Entry component ID", entry, setEntry)}
                </div>
                <p className="text-sm text-muted-foreground">
                  The baseline endpoint must use this installation’s preview
                  domain, scheme and port. Validation checks the supplied
                  ingress configuration.
                </p>
                {rows.map((row, index) => (
                  <fieldset
                    key={index}
                    className="rounded border border-border p-4 space-y-3"
                  >
                    <legend>Component {index + 1}</legend>
                    <div className="grid gap-3 sm:grid-cols-2">
                      {(
                        [
                          ["Component ID", "id"],
                          ["Service name", "service"],
                          ["Service port", "port"],
                          ["Baseline image", "image"],
                          ["Repository (optional)", "repository"],
                        ] as const
                      ).map(([label, key]) => (
                        <TextField
                          key={key}
                          label={`${label} ${index + 1}`}
                          value={row[key]}
                          type={key === "port" ? "number" : "text"}
                          onChange={(value) => update(index, { [key]: value })}
                        />
                      ))}
                    </div>
                    <label className="flex gap-2">
                      <input
                        type="checkbox"
                        checked={row.overridable}
                        onChange={(event) =>
                          update(index, { overridable: event.target.checked })
                        }
                      />
                      Allow overrides for component {index + 1}
                    </label>
                    <Button
                      type="button"
                      variant="ghost"
                      disabled={rows.length === 1}
                      onClick={() => {
                        invalidate();
                        setRows(rows.filter((_, i) => i !== index));
                      }}
                    >
                      Remove component {index + 1}
                    </Button>
                  </fieldset>
                ))}
                <Button
                  type="button"
                  variant="outline"
                  disabled={rows.length >= 20}
                  onClick={() => {
                    invalidate();
                    setRows([...rows, newComponent()]);
                  }}
                >
                  Add component
                </Button>
                <label className="block space-y-1">
                  Verification
                  <select
                    className="envy-select"
                    value={verification}
                    onChange={(event) => {
                      invalidate();
                      setVerification(
                        event.target.value as "http" | "envy-chain",
                      );
                    }}
                  >
                    <option value="http">HTTP reachability</option>
                    <option value="envy-chain">Instrumented Envy chain</option>
                  </select>
                </label>
                {verification === "http" ? (
                  <div className="grid gap-3 sm:grid-cols-2">
                    {field("Probe path", path, setPath)}
                    {field("Expected HTTP status", status, setStatus, "number")}
                  </div>
                ) : (
                  field(
                    "Ordered component IDs (comma-separated)",
                    chain,
                    setChain,
                  )
                )}
                <p className="text-sm">
                  HTTP success proves reachability. Application checks must
                  establish context propagation and override selection. Envy
                  chain verification requires application instrumentation.
                </p>
              </>
            )}
            {stage === 1 && (
              <>
                {rows.map((row, index) => (
                  <fieldset
                    key={index}
                    className="rounded border border-border p-4 space-y-3"
                  >
                    <legend>{row.id || `Component ${index + 1}`}</legend>
                    <label className="block space-y-1">
                      Profile for {row.id || index + 1}
                      <select
                        className="envy-select"
                        value={row.profile}
                        onChange={(event) =>
                          update(index, {
                            profile: event.target
                              .value as ComponentDraft["profile"],
                          })
                        }
                      >
                        <option value="deployment">
                          Derive from Deployment
                        </option>
                        <option value="deployment-composite">
                          Composite Deployment (operator policy required)
                        </option>
                        <option value="http-small">Manual HTTP workload</option>
                      </select>
                    </label>
                    {isDeploymentProfile(row.profile) ? (
                      <p>
                        Deployment discovery supplies workload configuration
                        after registration. Override creation requires an
                        approved profile.
                      </p>
                    ) : (
                      <>
                        <div className="grid gap-3 sm:grid-cols-2">
                          <TextField
                            label={`Health path ${index + 1}`}
                            value={row.health}
                            onChange={(health) => update(index, { health })}
                          />
                          <TextField
                            label={`Readiness path ${index + 1}`}
                            value={row.readiness}
                            onChange={(readiness) =>
                              update(index, { readiness })
                            }
                          />
                        </div>
                        <p>
                          Environment values are visible in the catalog. Do not
                          enter credentials. Registry Secret names must already
                          be approved by the installation.
                        </p>
                        <KeyValues
                          label={`Environment ${index + 1}`}
                          entries={row.env}
                          onChange={(env) => update(index, { env })}
                        />
                        <TextField
                          label={`Image-pull Secret names ${index + 1} (comma-separated)`}
                          value={row.secrets}
                          onChange={(secrets) => update(index, { secrets })}
                        />
                      </>
                    )}
                  </fieldset>
                ))}
                <p>
                  Registration durably saves immutable catalog entries. Matching
                  entries can be reused; changes to existing entries are
                  rejected. Registration does not create a preview.
                </p>
                {report && (
                  <div role="status" className="space-y-2">
                    {report.checks.map((check) => (
                      <p key={check.type}>
                        {check.type}: {check.status ? "Passed" : "Not passed"} —{" "}
                        {check.message}
                      </p>
                    ))}
                    {report.warnings.map((warning, index) => (
                      <p key={index}>{warning}</p>
                    ))}
                    <details>
                      <summary>Validated registration</summary>
                      <pre className="overflow-auto max-h-80 text-xs">
                        {JSON.stringify(report.configuration, null, 2)}
                      </pre>
                    </details>
                  </div>
                )}
              </>
            )}
            <div className="flex gap-3">
              {stage === 1 && (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setStage(0)}
                >
                  Back
                </Button>
              )}
              <Button type="submit">
                {busy
                  ? "Checking…"
                  : stage === 0
                    ? "Continue to configuration"
                    : "Validate configuration"}
              </Button>
              {stage === 1 && report && (
                <Button type="button" onClick={() => void submit(true)}>
                  Register baseline
                </Button>
              )}
            </div>
          </fieldset>
        </form>
      )}
      {saved && stage === 2 && (
        <>
          <p className="envy-panel">
            Baseline {saved.project}/{saved.id} is registered. You can leave and
            resume from Catalog. Unsaved preparation edits are discarded when
            you leave.
          </p>
          {savedComponents
            .filter((c) => c.overridable)
            .map((component) =>
              isDeploymentProfile(component.profile) ? (
                <PreviewPreparation
                  key={`${saved.project}/${saved.id}/${component.id}`}
                  project={saved.project}
                  baseline={saved.id}
                  component={component.id}
                />
              ) : (
                <div key={component.id} className="envy-panel">
                  <h3>{component.id}</h3>
                  <p>
                    Manual HTTP profile registered. No additional preparation is
                    required.
                  </p>
                </div>
              ),
            )}
          <Button onClick={() => setStage(3)}>Continue to first preview</Button>
        </>
      )}
      {saved && stage === 3 && (
        <div className="envy-panel space-y-4">
          <p>
            Use an approved service and a published build or image. Services
            still awaiting preparation may stay inherited. Deployment-derived
            overrides require an immutable image digest or a published build.
          </p>
          <p>
            The next screen lets you review selections and lifetime before
            explicitly creating a preview. HTTP checks prove reachability, not
            downstream routing.
          </p>
          <div className="flex gap-3">
            <Button variant="outline" onClick={() => setStage(2)}>
              Back to preparation
            </Button>
            <Button
              onClick={() =>
                onCreate({ project: saved.project, baseline: saved.id })
              }
            >
              Choose preview changes
            </Button>
          </div>
        </div>
      )}
      <Button variant="ghost" disabled={busy} onClick={onClose}>
        Close onboarding
      </Button>
    </div>
  );
}
