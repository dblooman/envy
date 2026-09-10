import { useEffect, useState } from "react";
import { useEnvyApi } from "../../context/ApiContext";
import { apiClient } from "../../lib/api-client";
import { SourceRepository } from "../../types/api";
import { Button } from "../ui/button";
import { Input } from "../ui/input";

export function SourceRepositories() {
  const { projects, components, isDemoMode, serverUrl, token } = useEnvyApi();
  const [project, setProject] = useState("");
  const projectId = project || projects[0]?.id || "";
  const [items, setItems] = useState<SourceRepository[]>([]);
  const [id, setId] = useState("");
  const [github, setGithub] = useState("");
  const [installation, setInstallation] = useState("");
  const [images, setImages] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    let current = true;
    setItems([]);
    setError("");
    if (!projectId || isDemoMode) return;
    setBusy(true);
    apiClient
      .listSourceRepositories(projectId)
      .then((rows) => {
        if (current) setItems(rows);
      })
      .catch((e: Error) => {
        if (current) setError(e.message);
      })
      .finally(() => {
        if (current) setBusy(false);
      });
    return () => {
      current = false;
    };
  }, [projectId, isDemoMode, serverUrl, token, refresh]);
  async function change(action: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await action();
      setRefresh((n) => n + 1);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Repository operation failed");
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="rounded-xl border border-border bg-card p-5 space-y-4">
      <div>
        <h3 className="font-semibold">Source repositories</h3>
        <p className="text-xs text-muted-foreground">
          Enable selected GitHub App repositories and map components to approved
          registry locations. Configure the App and CI credentials on the
          server.
        </p>
      </div>
      {isDemoMode ? (
        <p className="text-sm">
          Repository registration and Git lookup require Live Mode.
        </p>
      ) : (
        <>
          <label className="block text-xs">
            Project
            <select
              className="block mt-1 rounded border border-input bg-card p-2"
              value={projectId}
              disabled={busy}
              onChange={(e) => {
                setProject(e.target.value);
                setImages({});
              }}
            >
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>
          {error && (
            <p role="alert" className="text-sm text-rose-600">
              {error}
            </p>
          )}
          {items.map((r) => (
            <div
              key={r.id}
              className="rounded border border-border p-3 text-xs space-y-2"
            >
              <div className="flex justify-between gap-2 items-center">
                <span>
                  {r.github_repository} · {r.id} ·{" "}
                  {r.enabled ? "Enabled" : "Disabled"}
                </span>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  onClick={() =>
                    void change(() =>
                      apiClient.enableSourceRepository(
                        projectId,
                        r.id,
                        !r.enabled,
                      ),
                    )
                  }
                >
                  {r.enabled ? "Disable" : "Enable"}
                </Button>
              </div>
              {Object.entries(r.images).map(([c, image]) => (
                <p className="font-mono break-all" key={c}>
                  {c} → {image}
                </p>
              ))}
            </div>
          ))}
          <details>
            <summary className="cursor-pointer text-sm font-medium">
              Register repository
            </summary>
            <form
              className="mt-3 space-y-3"
              onSubmit={(e) => {
                e.preventDefault();
                void change(() =>
                  apiClient.registerSourceRepository({
                    project: projectId,
                    id,
                    github_repository: github,
                    installation_id: Number(installation),
                    enabled: true,
                    images,
                  }),
                );
              }}
            >
              <div className="grid gap-3 md:grid-cols-3">
                <label className="text-xs">
                  Repository ID
                  <Input
                    aria-label="Repository ID"
                    value={id}
                    onChange={(e) => setId(e.target.value)}
                    placeholder="shop-backend"
                    required
                  />
                </label>
                <label className="text-xs">
                  GitHub owner/repo
                  <Input
                    aria-label="GitHub repository"
                    value={github}
                    onChange={(e) => setGithub(e.target.value)}
                    placeholder="acme/shop"
                    required
                  />
                </label>
                <label className="text-xs">
                  App installation ID
                  <Input
                    aria-label="GitHub installation ID"
                    type="number"
                    min="1"
                    value={installation}
                    onChange={(e) => setInstallation(e.target.value)}
                    required
                  />
                </label>
              </div>
              {components
                .filter(
                  (c) =>
                    c.project === projectId &&
                    c.overridable &&
                    !items.some((r) => Object.hasOwn(r.images, c.id)),
                )
                .map((c) => (
                  <div key={c.id} className="flex items-center gap-3 text-xs">
                    <label className="flex gap-2">
                      <input
                        type="checkbox"
                        checked={Object.hasOwn(images, c.id)}
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
                    {Object.hasOwn(images, c.id) && (
                      <Input
                        aria-label={`${c.id} registry location`}
                        value={images[c.id]}
                        placeholder="registry.example.com/team/service (no tag)"
                        required
                        onChange={(e) =>
                          setImages((old) => ({
                            ...old,
                            [c.id]: e.target.value,
                          }))
                        }
                      />
                    )}
                  </div>
                ))}
              <p className="text-xs text-muted-foreground">
                Repository identity and component mappings are immutable after
                registration. Disabling blocks new selections and leaves
                existing compositions running.
              </p>
              <Button
                type="submit"
                size="sm"
                disabled={busy || !projectId || !Object.keys(images).length}
              >
                {busy ? "Checking…" : "Validate access and register"}
              </Button>
            </form>
          </details>
        </>
      )}
    </section>
  );
}
