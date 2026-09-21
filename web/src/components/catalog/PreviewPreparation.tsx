import { useEffect, useRef, useState } from "react";
import { ApiRequestError, apiClient } from "../../lib/api-client";
import type {
  PreviewProfile,
  PreviewReport,
  PreviewSelection,
} from "../../types/api";
import { Button } from "../ui/button";
import { entryMap, KeyValues, TextField, type Entry } from "./OnboardingFields";

export function PreviewPreparation({
  project,
  baseline,
  component,
}: {
  project: string;
  baseline: string;
  component: string;
}) {
  const [profile, setProfile] = useState<PreviewProfile | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [initialLoading, setInitialLoading] = useState(true);
  const [report, setReport] = useState<PreviewReport>();
  const [deployment, setDeployment] = useState("");
  const [container, setContainer] = useState("");
  const [env, setEnv] = useState<Entry[]>([]);
  const [maps, setMaps] = useState<
    { name: string; key: string; value: string }[]
  >([]);
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const alive = useRef(false);
  const pending = useRef(false);
  const adopt = (selection: PreviewSelection) => {
    setDeployment(selection.deployment || "");
    setContainer(selection.container || "");
    setEnv(
      Object.entries(selection.env || {}).map(([key, value]) => ({
        key,
        value,
      })),
    );
    setMaps(
      Object.entries(selection.config_map_keys || {}).flatMap(
        ([name, values]) =>
          Object.entries(values).map(([key, value]) => ({ name, key, value })),
      ),
    );
  };
  const inspect = async () => {
    try {
      return await apiClient.inspectPreviewProfile(
        project,
        baseline,
        component,
      );
    } catch (err) {
      if (err instanceof ApiRequestError && err.status === 404) return null;
      throw err;
    }
  };
  useEffect(() => {
    alive.current = true;
    let cancelled = false;
    inspect()
      .then((value) => {
        if (cancelled) return;
        setProfile(value);
        if (value) adopt(value.selection);
        setLoaded(true);
      })
      .catch((err) => {
        if (!cancelled) setError(String(err));
      })
      .finally(() => {
        if (!cancelled) setInitialLoading(false);
      });
    return () => {
      cancelled = true;
      alive.current = false;
    };
    // This editor is keyed by project/baseline/component and connection scope.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const invalidate = () => {
    setReport(undefined);
    setConfirmed(false);
    setError("");
    setMessage("");
  };
  const selection = (): PreviewSelection => {
    const config: Record<string, Record<string, string>> = Object.create(null);
    for (const row of maps) {
      if (!row.name.trim() || !row.key.trim())
        throw new Error("ConfigMap names and keys are required.");
      const values = (config[row.name.trim()] ||= Object.create(null));
      if (Object.hasOwn(values, row.key.trim()))
        throw new Error("ConfigMap keys must be unique within each ConfigMap.");
      values[row.key.trim()] = row.value;
    }
    return {
      ...(deployment.trim() ? { deployment: deployment.trim() } : {}),
      ...(container.trim() ? { container: container.trim() } : {}),
      env: entryMap(env),
      config_map_keys: config,
    };
  };
  const run = async (approve: boolean) => {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      if (approve && report && confirmed) {
        const saved = await apiClient.approvePreviewProfile(
          project,
          baseline,
          component,
          {
            selection: report.selection,
            inspection: report.inspection,
            expected_revision: profile?.revision || 0,
            confirm_connectivity: true,
          },
        );
        if (!alive.current) return;
        setProfile(saved);
        setReport(undefined);
        setConfirmed(false);
        setMessage(`Approved revision ${saved.revision}.`);
      } else {
        const current = await inspect();
        if (!alive.current) return;
        setProfile(current);
        setLoaded(true);
        const result = await apiClient.discoverPreviewProfile(
          project,
          baseline,
          component,
          selection(),
        );
        if (!alive.current) return;
        setReport(result);
        adopt(result.selection);
        setConfirmed(false);
      }
    } catch (err) {
      if (!alive.current) return;
      setError(err instanceof Error ? err.message : String(err));
      setReport(undefined);
      setConfirmed(false);
      if (approve) {
        try {
          const current = await inspect();
          if (alive.current) {
            setProfile(current);
            setLoaded(true);
            setMessage(
              "Approval was not confirmed. Review the saved revision, discover again, and review fresh evidence before approving.",
            );
          }
        } catch {
          if (alive.current) {
            setLoaded(false);
            setMessage(
              "Could not refresh the saved revision. Retry discovery before approval.",
            );
          }
        }
      }
    } finally {
      pending.current = false;
      if (alive.current) setBusy(false);
    }
  };
  return (
    <section
      className="envy-panel space-y-4"
      aria-label={`Prepare ${component}`}
    >
      <h3 className="font-semibold">{component}</h3>
      <p role="status">
        {loaded
          ? profile
            ? `Approved profile revision ${profile.revision}`
            : "Preparation required"
          : initialLoading
            ? "Checking saved profile…"
            : "Approval status unavailable; retry discovery."}
      </p>
      {error && (
        <p role="alert" className="envy-error-banner">
          {error}
        </p>
      )}
      {message && <p role="status">{message}</p>}
      <fieldset disabled={busy || initialLoading} className="space-y-4">
        <p className="text-sm text-muted-foreground">
          Discover the deployed configuration, or deliberately review a new
          approval. Existing approvals remain saved while you edit.
        </p>
        <div className="grid gap-3 sm:grid-cols-2">
          <TextField
            label="Deployment (optional)"
            value={deployment}
            onChange={(value) => {
              invalidate();
              setDeployment(value);
            }}
          />
          <TextField
            label="Container (optional)"
            value={container}
            onChange={(value) => {
              invalidate();
              setContainer(value);
            }}
          />
        </div>
        <p className="text-sm">
          Replacements may only change existing literal variables or text keys
          in referenced ConfigMaps. These values are public catalog
          configuration; do not enter credentials.
        </p>
        <KeyValues
          label="Environment replacement"
          entries={env}
          onChange={(value) => {
            invalidate();
            setEnv(value);
          }}
        />
        <fieldset className="space-y-2">
          <legend>ConfigMap replacements</legend>
          {maps.map((row, index) => (
            <div key={index} className="grid gap-2 sm:grid-cols-4">
              {(["name", "key", "value"] as const).map((field) => (
                <TextField
                  key={field}
                  label={`ConfigMap ${field} ${index + 1}`}
                  value={row[field]}
                  onChange={(value) => {
                    invalidate();
                    setMaps(
                      maps.map((old, i) =>
                        i === index ? { ...old, [field]: value } : old,
                      ),
                    );
                  }}
                />
              ))}
              <Button
                type="button"
                variant="ghost"
                onClick={() => {
                  invalidate();
                  setMaps(maps.filter((_, i) => i !== index));
                }}
              >
                Remove ConfigMap replacement {index + 1}
              </Button>
            </div>
          ))}
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              invalidate();
              setMaps([...maps, { name: "", key: "", value: "" }]);
            }}
          >
            Add ConfigMap replacement
          </Button>
        </fieldset>
        <Button type="button" variant="outline" onClick={() => void run(false)}>
          {busy ? "Working…" : "Discover configuration"}
        </Button>
        {report && (
          <div className="space-y-3">
            <p>
              Source: {report.source.namespace}/{report.source.deployment} ·
              generation {report.source.generation}
            </p>
            {report.blockers.map((blocker, i) => (
              <p role="alert" key={i}>
                {blocker}
              </p>
            ))}
            {report.warnings.map((warning, i) => (
              <p key={i} className="text-sm">
                {warning}
              </p>
            ))}
            {report.connectivity?.map((finding, i) => (
              <p key={i}>
                <strong>{finding.location}</strong>: {finding.message}{" "}
                {finding.replacement &&
                  `Suggested replacement: ${finding.replacement}`}
              </p>
            ))}
            {report.composite_policy && (
              <details open>
                <summary>Approved container and identity policy</summary>
                <p>
                  {report.composite_policy_key} · revision{" "}
                  {report.composite_policy.revision}
                </p>
                <pre className="overflow-auto text-xs max-h-80">
                  {JSON.stringify(report.composite_policy, null, 2)}
                </pre>
              </details>
            )}
            <details open>
              <summary>Configuration and dependencies</summary>
              <pre className="overflow-auto text-xs max-h-80">
                {JSON.stringify(
                  {
                    configuration: report.configuration,
                    dependencies: report.dependencies,
                  },
                  null,
                  2,
                )}
              </pre>
            </details>
            <details open>
              <summary>Required source-read permission rules</summary>
              <pre className="overflow-auto text-xs">
                {JSON.stringify(report.source_read_rules, null, 2)}
              </pre>
              <p className="text-sm">
                Arrange these named permissions in namespace{" "}
                {report.source.namespace}, bound to the Envy controller service
                account. Then retry discovery. Envy does not grant these
                permissions here.
              </p>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  void navigator.clipboard
                    .writeText(
                      JSON.stringify(report.source_read_rules, null, 2),
                    )
                    .then(() => {
                      if (alive.current) setMessage("Permission rules copied.");
                    })
                    .catch(() => {
                      if (alive.current)
                        setError(
                          "Could not copy. Select and copy the permission rules above.",
                        );
                    });
                }}
              >
                Copy permission rules
              </Button>
            </details>
            <label className="flex gap-2">
              <input
                type="checkbox"
                checked={confirmed}
                onChange={(event) => setConfirmed(event.target.checked)}
              />
              I have checked dependency addresses, shared side effects,
              service-link assumptions, and mesh context propagation.
            </label>
            <Button
              type="button"
              disabled={!loaded || !confirmed || report.blockers.length > 0}
              onClick={() => void run(true)}
            >
              Approve inspected configuration
            </Button>
          </div>
        )}
      </fieldset>
    </section>
  );
}
