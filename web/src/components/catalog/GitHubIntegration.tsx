import { useCallback, useEffect, useState } from "react";
import { useEnvyApi } from "../../context/ApiContext";
import { apiClient } from "../../lib/api-client";
import type {
  GitHubStatus,
  GitHubInstallation,
  PreviewPolicy,
  PRPreview,
} from "../../types/github";
import type { SourceRepository } from "../../types/api";
import { Button } from "../ui/button";

export function GitHubIntegration() {
  const { projects, baselines, isDemoMode } = useEnvyApi();
  const [status, setStatus] = useState<GitHubStatus>();
  const [installations, setInstallations] = useState<GitHubInstallation[]>([]);
  const [previews, setPreviews] = useState<PRPreview[]>([]);
  const [repositories, setRepositories] = useState<SourceRepository[]>([]);
  const [policies, setPolicies] = useState<PreviewPolicy[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useState<PreviewPolicy>({
    project: "",
    repository: "",
    enabled: true,
    baseline: "",
    components: [],
    ttl: "8h",
    workflow_id: 0,
  });
  const refresh = useCallback(async () => {
    if (isDemoMode) return;
    try {
      const health = await apiClient.githubStatus();
      setStatus(health);
      if (!health.configured) return;
      const [policy, repo] = await Promise.all([
        apiClient.previewPolicies(),
        Promise.all(
          projects.map((p) => apiClient.listSourceRepositories(p.id)),
        ),
      ]);
      setPolicies(policy.items);
      setRepositories(repo.flat());
      const all: PRPreview[] = [];
      let cursor = "";
      do {
        const page = await apiClient.prPreviews(cursor);
        all.push(...page.items);
        cursor = page.next_cursor || "";
      } while (cursor);
      setPreviews(all);
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "Unable to load GitHub integration",
      );
    }
  }, [isDemoMode, projects]);
  useEffect(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), 15000);
    return () => clearInterval(timer);
  }, [refresh]);
  async function change(fn: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await fn();
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "GitHub operation failed");
    } finally {
      setBusy(false);
    }
  }
  const selected = repositories.find(
    (r) => r.project === draft.project && r.id === draft.repository,
  );
  return (
    <section className="space-y-5">
      <h3 className="font-semibold">GitHub PR previews</h3>
      <p>
        Add <code>envy-preview</code> to request an environment. It follows
        successful builds until expiry, label removal or PR closure.
        Independently created environments are unaffected.
      </p>
      {isDemoMode ? (
        <p>GitHub integration requires Live Mode.</p>
      ) : (
        <>
          <div className="rounded border p-4 text-sm space-y-2">
            <p>
              App: {status?.configured ? "Configured" : "Not configured"} ·
              Webhook:{" "}
              {status?.webhook_configured ? "Configured" : "Not configured"}
            </p>
            <p>Last delivery: {status?.last_webhook_at || "None"}</p>
            <p>
              Last successful reconciliation:{" "}
              {status?.last_reconciled_at || "None"}
            </p>
            {status?.error && <p role="alert">{status.error}</p>}
            <Button
              type="button"
              disabled={busy || !status?.configured}
              onClick={() =>
                void change(async () => {
                  const all: GitHubInstallation[] = [];
                  for (let page = 1; ; page++) {
                    const result = await apiClient.githubInstallations(page);
                    all.push(...result.items);
                    if (!result.has_more) break;
                  }
                  setInstallations(all);
                })
              }
            >
              Inspect App access
            </Button>
            {installations.map((installation) => (
              <div key={installation.id}>
                <p>
                  {installation.account.login} · installation {installation.id}
                </p>
                <p>
                  {Object.entries(installation.permissions)
                    .map(([permission, level]) => `${permission}: ${level}`)
                    .join(" · ")}
                </p>
              </div>
            ))}
            <p>
              Operator setup: mount the App private key and webhook secret, and
              subscribe the App to pull request and installation events at{" "}
              <code>/webhooks/github</code>.
            </p>
          </div>
          {error && <p role="alert">{error}</p>}
          <form
            className="rounded border p-4 space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              void change(() => apiClient.savePreviewPolicy(draft));
            }}
          >
            <h4 className="font-semibold">Repository preview policy</h4>
            <label className="block">
              Source repository{" "}
              <select
                required
                value={draft.project + "/" + draft.repository}
                onChange={(e) => {
                  const repo = repositories.find(
                    (r) => r.project + "/" + r.id === e.target.value,
                  );
                  if (repo)
                    setDraft(
                      policies.find(
                        (p) =>
                          p.project === repo.project &&
                          p.repository === repo.id,
                      ) || {
                        project: repo.project,
                        repository: repo.id,
                        enabled: true,
                        baseline: "",
                        components: [],
                        ttl: "8h",
                        workflow_id: 0,
                      },
                    );
                }}
              >
                <option value="/">Select repository</option>
                {repositories.map((r) => (
                  <option
                    key={r.project + "/" + r.id}
                    value={r.project + "/" + r.id}
                  >
                    {r.project} · {r.github_repository}
                  </option>
                ))}
              </select>
            </label>
            <label className="block">
              Baseline{" "}
              <select
                required
                value={draft.baseline}
                onChange={(e) =>
                  setDraft({ ...draft, baseline: e.target.value })
                }
              >
                <option value="">Select baseline</option>
                {baselines
                  .filter((b) => b.project === draft.project)
                  .map((b) => (
                    <option key={b.id} value={b.id}>
                      {b.id}
                    </option>
                  ))}
              </select>
            </label>
            <fieldset>
              <legend>Components (one to three)</legend>
              {Object.keys(selected?.images || {}).map((c) => (
                <label key={c} className="block">
                  <input
                    type="checkbox"
                    checked={draft.components.includes(c)}
                    onChange={(e) =>
                      setDraft({
                        ...draft,
                        components: e.target.checked
                          ? [...draft.components, c]
                          : draft.components.filter((x) => x !== c),
                      })
                    }
                  />
                  {c}
                </label>
              ))}
            </fieldset>
            <label className="block">
              Trusted Actions workflow ID{" "}
              <input
                required
                type="number"
                min="1"
                value={draft.workflow_id || ""}
                onChange={(e) =>
                  setDraft({ ...draft, workflow_id: Number(e.target.value) })
                }
              />
            </label>
            <label className="block">
              Lifetime{" "}
              <input
                required
                value={draft.ttl}
                onChange={(e) => setDraft({ ...draft, ttl: e.target.value })}
              />
            </label>
            <label className="block">
              <input
                type="checkbox"
                checked={draft.enabled}
                onChange={(e) =>
                  setDraft({ ...draft, enabled: e.target.checked })
                }
              />
              Enable labelled PR previews
            </label>
            <Button disabled={busy || !selected}>
              Validate permissions and save
            </Button>
          </form>
          <div className="space-y-3">
            <h4 className="font-semibold">PR environments</h4>
            {!previews.length && <p>No PR previews have been requested.</p>}
            {previews.map((p) => (
              <article key={p.id} className="rounded border p-4 space-y-2">
                <a href={p.pr_url} target="_blank" rel="noreferrer">
                  {p.policy.project}/{p.policy.repository} PR #{p.number}
                </a>
                <p>
                  {p.status} · {p.reason}
                </p>
                <p>Environment: {p.deployment_status || "Not created"}</p>
                <p className="break-all text-xs">
                  Requested: {p.requested_sha}
                  <br />
                  Deployed: {p.deployed_sha || "None"}
                </p>
                {p.url && (
                  <a href={p.url} target="_blank" rel="noreferrer">
                    Open preview
                  </a>
                )}
                <p className="text-xs">
                  Composition: {p.composition_id || "Not created"} · generation{" "}
                  {p.generation} · expires {p.expires_at || "after creation"}
                </p>
                {p.feedback_error && <p>{p.feedback_error}</p>}
                <Button
                  disabled={busy}
                  onClick={() =>
                    void change(() =>
                      apiClient.controlPRPreview(p.id, "restart"),
                    )
                  }
                >
                  Restart
                </Button>{" "}
                <Button
                  disabled={busy || p.terminal}
                  onClick={() =>
                    void change(() => apiClient.controlPRPreview(p.id, "stop"))
                  }
                >
                  Stop
                </Button>
              </article>
            ))}
          </div>
        </>
      )}
    </section>
  );
}
