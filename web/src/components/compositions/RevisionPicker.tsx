import { useEffect, useRef, useState } from "react";
import { apiClient } from "../../lib/api-client";
import { useEnvyApi } from "../../context/ApiContext";
import {
  ComponentOverride,
  GitBranch,
  GitCommit,
  RevisionResolution,
  SourceRepository,
} from "../../types/api";
import { Input } from "../ui/input";
import { Button } from "../ui/button";

// Form state encodes only the selected identity; provenance always comes from the server.
export function selectedOverride(value: string): ComponentOverride {
  return value.startsWith("build:")
    ? { build_id: value.slice(6) }
    : { image: value.trim() };
}

export function RevisionPicker({
  project,
  component,
  profile,
  value,
  onChange,
}: {
  project: string;
  component: string;
  profile?: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const { isDemoMode, serverUrl, token } = useEnvyApi();
  const [mode, setMode] = useState<"build" | "image">(
    isDemoMode || (value && !value.startsWith("build:")) ? "image" : "build",
  );
  const [repo, setRepo] = useState<SourceRepository>();
  const [branches, setBranches] = useState<GitBranch[]>([]);
  const [branchPage, setBranchPage] = useState(0);
  const [moreBranches, setMoreBranches] = useState(false);
  const [commits, setCommits] = useState<GitCommit[]>([]);
  const [commitPage, setCommitPage] = useState(0);
  const [moreCommits, setMoreCommits] = useState(false);
  const [refKind, setRefKind] = useState<"branch" | "sha">("branch");
  const [ref, setRef] = useState("");
  const [resolution, setResolution] = useState<RevisionResolution>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const serial = useRef(0);
  const change = useRef(onChange);
  change.current = onChange;

  useEffect(() => {
    const request = ++serial.current;
    setRepo(undefined);
    setResolution(undefined);
    setBranches([]);
    setCommits([]);
    setError("");
    if (mode !== "build" || isDemoMode) {
      setBusy(false);
      return;
    }
    change.current("");
    setBranchPage(0);
    setMoreBranches(false);
    setCommitPage(0);
    setMoreCommits(false);
    setBusy(true);
    apiClient
      .listSourceRepositories(project)
      .then((items) => {
        if (serial.current !== request) return;
        const match = items.find((r) => Object.hasOwn(r.images, component));
        setRepo(match);
        if (!match)
          setError(
            "No repository is registered for this component. Register it in Catalog first.",
          );
        else if (!match.enabled) setError("This repository is disabled.");
      })
      .catch((e: Error) => {
        if (serial.current === request) setError(e.message);
      })
      .finally(() => {
        if (serial.current === request) setBusy(false);
      });
    return () => {
      ++serial.current;
    };
  }, [project, component, mode, isDemoMode, serverUrl, token]);

  function invalidate() {
    ++serial.current;
    setBusy(false);
    setResolution(undefined);
    setError("");
    onChange("");
  }
  async function run(action: () => Promise<() => void>) {
    const request = ++serial.current;
    setBusy(true);
    setError("");
    try {
      const apply = await action();
      if (serial.current === request) apply();
    } catch (e) {
      if (serial.current === request)
        setError(e instanceof Error ? e.message : "Lookup failed");
    } finally {
      if (serial.current === request) setBusy(false);
    }
  }
  function resolve(target = ref, after = "") {
    if (!repo) return;
    if (refKind === "sha" && !/^[0-9a-f]{40}$/.test(target)) {
      setError("Enter a full 40-character lowercase Git commit SHA.");
      return;
    }
    onChange("");
    void run(async () => {
      const result = await apiClient.resolveRevision(
        project,
        repo.id,
        component,
        target,
        after,
      );
      return () =>
        setResolution(
          after && resolution
            ? { ...result, builds: [...resolution.builds, ...result.builds] }
            : result,
        );
    });
  }
  function loadBranches(page: number) {
    if (!repo) return;
    void run(async () => {
      const result = await apiClient.sourceBranches(project, repo.id, page);
      return () => {
        setBranches(page === 1 ? result.items : [...branches, ...result.items]);
        setBranchPage(page);
        setMoreBranches(result.has_more);
      };
    });
  }
  function loadCommits(page: number) {
    if (!repo) return;
    void run(async () => {
      const result = await apiClient.sourceCommits(project, repo.id, ref, page);
      return () => {
        setCommits(page === 1 ? result.items : [...commits, ...result.items]);
        setCommitPage(page);
        setMoreCommits(result.has_more);
      };
    });
  }
  const chosen = resolution?.builds.find((b) => value === `build:${b.id}`);
  return (
    <div className="space-y-2 text-xs">
      <label className="block">
        {component} version
        <select
          aria-label={`${component} version source`}
          className="w-full mt-1 rounded border border-input bg-card p-2"
          value={mode}
          onChange={(e) => {
            invalidate();
            setMode(e.target.value as "build" | "image");
          }}
        >
          <option value="build" disabled={isDemoMode}>
            Published Git build{isDemoMode ? " (Live Mode)" : ""}
          </option>
          <option value="image">
            Direct image (source provenance unavailable)
          </option>
        </select>
      </label>
      {mode === "image" ? (
        <Input
          aria-label={`${component} image`}
          value={value.startsWith("build:") ? "" : value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="registry/application:version"
          required
        />
      ) : (
        <>
          {repo && (
            <p className="break-all">
              {repo.github_repository} · {repo.enabled ? "Enabled" : "Disabled"}
            </p>
          )}
          {repo?.enabled && (
            <>
              <div className="flex gap-2">
                <select
                  aria-label={`${component} revision type`}
                  className="rounded border border-input bg-card p-2"
                  value={refKind}
                  onChange={(e) => {
                    invalidate();
                    setRefKind(e.target.value as "branch" | "sha");
                    setRef("");
                    setCommits([]);
                  }}
                >
                  <option value="branch">Branch</option>
                  <option value="sha">Commit SHA</option>
                </select>
                <Input
                  aria-label={`${component} Git revision`}
                  list={
                    refKind === "branch" ? `branches-${component}` : undefined
                  }
                  value={ref}
                  onChange={(e) => {
                    invalidate();
                    setRef(e.target.value);
                    setCommits([]);
                    setMoreCommits(false);
                  }}
                  placeholder={
                    refKind === "branch"
                      ? "main or feature/name"
                      : "Full Git commit SHA"
                  }
                />
                <datalist id={`branches-${component}`}>
                  {branches.map((b) => (
                    <option key={b.name} value={b.name} />
                  ))}
                </datalist>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={busy || !ref.trim()}
                  onClick={() => resolve()}
                >
                  Resolve revision
                </Button>
                {refKind === "branch" && (
                  <>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={busy}
                      onClick={() =>
                        loadBranches(moreBranches ? branchPage + 1 : 1)
                      }
                    >
                      {moreBranches ? "More branches" : "Browse branches"}
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={busy || !ref.trim()}
                      onClick={() => loadCommits(1)}
                    >
                      Browse commits
                    </Button>
                  </>
                )}
              </div>
              {branches.length > 0 && refKind === "branch" && (
                <select
                  aria-label={`${component} branch`}
                  className="w-full rounded border border-input bg-card p-2"
                  value=""
                  onChange={(e) => {
                    invalidate();
                    setRef(e.target.value);
                    setCommits([]);
                    setMoreCommits(false);
                  }}
                >
                  <option value="">Select a branch…</option>
                  {branches.map((b) => (
                    <option key={b.name} value={b.name}>
                      {b.name}
                    </option>
                  ))}
                </select>
              )}
              {commits.length > 0 && (
                <select
                  aria-label={`${component} historical commit`}
                  className="w-full rounded border border-input bg-card p-2"
                  value=""
                  onChange={(e) => {
                    invalidate();
                    setRefKind("sha");
                    setRef(e.target.value);
                    setCommits([]);
                    resolve(e.target.value);
                  }}
                >
                  <option value="">Select a historical commit…</option>
                  {commits.map((c) => (
                    <option key={c.sha} value={c.sha}>
                      {c.sha.slice(0, 10)} — {c.message.split("\n")[0]}
                    </option>
                  ))}
                </select>
              )}
              {moreCommits && refKind === "branch" && (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={busy}
                  onClick={() => loadCommits(commitPage + 1)}
                >
                  Older commits
                </Button>
              )}
              {resolution && (
                <div className="rounded border border-border p-3 space-y-2">
                  <p className="font-medium">Resolved commit (pinned)</p>
                  <p className="font-mono break-all">{resolution.commit.sha}</p>
                  <p className="wrap-break-word">
                    {resolution.commit.message.split("\n")[0]}
                  </p>
                  {resolution.builds.length === 0 ? (
                    <p>
                      Commit exists; no published build available.{" "}
                      <a
                        className="underline"
                        href={resolution.ci_url}
                        target="_blank"
                        rel="noreferrer"
                      >
                        Open CI
                      </a>
                      , then refresh.
                    </p>
                  ) : (
                    <label className="block">
                      Published build
                      <select
                        aria-label={`${component} published build`}
                        className="w-full mt-1 rounded border border-input bg-card p-2"
                        value={chosen?.id || ""}
                        disabled={busy}
                        onChange={(e) =>
                          onChange(
                            e.target.value ? `build:${e.target.value}` : "",
                          )
                        }
                      >
                        <option value="">Select an exact build…</option>
                        {resolution.builds.map((b) => (
                          <option key={b.id} value={b.id}>
                            {new Date(b.built_at).toLocaleString()} · run{" "}
                            {b.run_id}/{b.attempt} ·{" "}
                            {b.image.split("@")[1]?.slice(0, 19)}
                          </option>
                        ))}
                      </select>
                    </label>
                  )}
                  {resolution.next_cursor && (
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={busy}
                      onClick={() =>
                        resolve(resolution.commit.sha, resolution.next_cursor)
                      }
                    >
                      More builds
                    </Button>
                  )}
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={busy}
                    onClick={() => resolve(resolution.commit.sha)}
                  >
                    Refresh builds for this commit
                  </Button>
                  {chosen && (
                    <>
                      <p className="font-mono break-all">{chosen.image}</p>
                      <a
                        className="underline"
                        href={chosen.run_url}
                        target="_blank"
                        rel="noreferrer"
                      >
                        View CI run
                      </a>
                    </>
                  )}
                </div>
              )}
            </>
          )}
          {busy && <p role="status">Looking up GitHub / build catalog…</p>}
          {error && (
            <p role="alert" className="text-rose-600 dark:text-rose-400">
              {error}
            </p>
          )}
        </>
      )}
      <p className="text-muted-foreground">
        Approved profile: {profile || "registered profile"}. Shared baseline
        remains live.
      </p>
    </div>
  );
}
