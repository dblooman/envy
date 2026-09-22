import {
  ApplicationOnboarding,
  type OnboardingTarget,
} from "./ApplicationOnboarding";
import { TopologyView } from "../topology/TopologyView";
import { useState } from "react";
import { SourceRepositories } from "./SourceRepositories";
import { GitHubIntegration } from "./GitHubIntegration";
import {
  Boxes,
  Server,
  GitBranch,
  CheckCircle2,
  ShieldCheck,
  Lock,
} from "lucide-react";
import { useEnvyApi } from "../../context/ApiContext";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "../ui/card";
import { CatalogRegistration } from "./CatalogRegistration";
import { Badge } from "../ui/badge";

export function CatalogView({
  onCreate,
  initialTarget,
}: {
  onCreate?: (target: OnboardingTarget) => void;
  initialTarget?: OnboardingTarget;
}) {
  const { isDemoMode, serverStatus, session, installation } = useEnvyApi();
  const scope = JSON.stringify([
    isDemoMode,
    serverStatus,
    installation?.id,
    session,
  ]);
  return (
    <ScopedCatalog
      key={scope}
      onCreate={onCreate}
      initialTarget={initialTarget}
    />
  );
}
function ScopedCatalog({
  onCreate,
  initialTarget,
}: {
  onCreate?: (target: OnboardingTarget) => void;
  initialTarget?: OnboardingTarget;
}) {
  const { isDemoMode, serverStatus, baselines, loading } = useEnvyApi();
  const [onboarding, setOnboarding] = useState(!!initialTarget);
  const [resume, setResume] = useState<OnboardingTarget | undefined>(
    initialTarget,
  );
  const start = (target?: OnboardingTarget) => {
    setResume(target);
    setOnboarding(true);
  };
  if (
    onboarding &&
    resume &&
    !baselines.some(
      (b) => b.project === resume.project && b.id === resume.baseline,
    )
  )
    return (
      <div className="envy-panel">
        <p role="status">
          {loading
            ? "Loading saved baseline…"
            : "The selected baseline is unavailable in this installation."}
        </p>
        <button onClick={() => setOnboarding(false)}>Back to Catalog</button>
      </div>
    );
  if (onboarding)
    return isDemoMode || serverStatus !== "connected" ? (
      <div className="envy-panel">
        <p>Connect to a live installation to onboard an application.</p>
        <button onClick={() => setOnboarding(false)}>Back to Catalog</button>
      </div>
    ) : (
      <ApplicationOnboarding
        resume={resume}
        onCreate={(target) => onCreate?.(target)}
        onClose={() => setOnboarding(false)}
      />
    );
  return <CatalogContents start={start} />;
}
function CatalogContents({
  start,
}: {
  start: (target?: OnboardingTarget) => void;
}) {
  const { projects, baselines, components } = useEnvyApi();
  const [section, setSection] = useState("Catalog");
  const [projectFilter, setProjectFilter] = useState("");

  return (
    <div className="space-y-8">
      <div className="envy-admin-intro">
        <ShieldCheck size={22} />
        <div>
          <strong>Approved foundations for developer previews</strong>
          <p>
            Inspect registered components and baselines, manage source
            repositories, or register catalog entries.
          </p>
        </div>
      </div>
      <button className="envy-panel text-primary" onClick={() => start()}>
        Onboard application
      </button>
      <nav className="envy-section-nav" aria-label="Catalog sections">
        {["Catalog", "Sources", "GitHub", "Advanced registration"].map(
          (item) => (
            <button
              key={item}
              aria-current={section === item ? "page" : undefined}
              onClick={() => setSection(item)}
            >
              {item === "Catalog" ? "Services & baselines" : item}
            </button>
          ),
        )}
      </nav>
      {section === "Catalog" && (
        <label className="block">
          Project
          <select
            className="envy-input ml-3"
            value={projectFilter}
            onChange={(e) => setProjectFilter(e.target.value)}
          >
            <option value="">All projects</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
      )}
      {section === "Advanced registration" && <CatalogRegistration />}
      {section === "Sources" && <SourceRepositories />}
      {section === "GitHub" && <GitHubIntegration />}
      {section === "Catalog" && (
        <>
          {!projects.length && (
            <p className="envy-panel text-sm text-muted-foreground">
              No projects are available.{" "}
              <button
                className="text-primary underline"
                onClick={() => start()}
              >
                Onboard your first application
              </button>{" "}
              to get started.
            </p>
          )}

          {/* Overview Cards */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {projects
              .filter((p) => !projectFilter || p.id === projectFilter)
              .map((proj) => (
                <Card
                  key={proj.id}
                  className="border-border bg-card shadow-2xs"
                >
                  <CardHeader className="pb-3">
                    <div className="flex flex-wrap gap-3 items-center justify-between">
                      <CardTitle className="text-base flex flex-wrap items-center gap-2 wrap-anywhere">
                        <Boxes className="h-4 w-4 text-foreground" />
                        {proj.name}
                      </CardTitle>
                      <span className="font-mono text-xs px-2 py-0.5 rounded bg-muted text-muted-foreground border border-border">
                        {proj.id}
                      </span>
                    </div>
                    <CardDescription>
                      Logical project scope for ephemeral compositions and
                      baselines.
                    </CardDescription>
                  </CardHeader>
                  <CardContent className="text-xs text-muted-foreground space-y-1">
                    <div className="flex justify-between">
                      <span>Total Components:</span>
                      <span className="font-mono font-medium text-foreground">
                        {components.filter((c) => c.project === proj.id).length}
                      </span>
                    </div>
                    <div className="flex justify-between">
                      <span>Active Baselines:</span>
                      <span className="font-mono font-medium text-foreground">
                        {baselines.filter((b) => b.project === proj.id).length}
                      </span>
                    </div>
                  </CardContent>
                </Card>
              ))}

            {baselines
              .filter((b) => !projectFilter || b.project === projectFilter)
              .map((baseline) => (
                <Card
                  key={`${baseline.project}/${baseline.id}`}
                  className="border-border bg-card shadow-2xs md:col-span-2"
                >
                  <CardHeader className="pb-3">
                    <div className="flex flex-wrap gap-3 items-center justify-between">
                      <CardTitle className="text-base flex flex-wrap items-center gap-2 wrap-anywhere">
                        <Server className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
                        Shared Baseline:{" "}
                        <span className="text-foreground wrap-anywhere font-semibold">
                          {baseline.project}/{baseline.id}
                        </span>
                      </CardTitle>
                      <Badge
                        variant="outline"
                        className="font-mono text-[11px]"
                      >
                        Revision: {baseline.revision}
                      </Badge>
                    </div>
                    <CardDescription>
                      Inherited baseline environment. State, databases, caches,
                      and baseline pods remain shared.
                    </CardDescription>
                  </CardHeader>
                  <CardContent className="text-xs space-y-2">
                    <details>
                      <summary>Routing configuration</summary>
                      <TopologyView
                        baselineId={baseline.id}
                        projectId={baseline.project}
                      />
                    </details>
                    <button
                      className="text-primary underline"
                      onClick={() =>
                        start({
                          project: baseline.project,
                          baseline: baseline.id,
                        })
                      }
                    >
                      Prepare overrides for {baseline.project}/{baseline.id}
                    </button>
                    <div className="flex items-center gap-2 bg-muted/40 p-2.5 rounded-lg border border-border font-mono text-xs">
                      <span className="text-muted-foreground">Endpoint:</span>
                      <a
                        href={baseline.endpoint}
                        target="_blank"
                        rel="noreferrer"
                        className="text-primary hover:underline font-medium wrap-anywhere min-w-0"
                      >
                        {baseline.endpoint}
                      </a>
                    </div>
                  </CardContent>
                </Card>
              ))}
          </div>

          {/* Component Profiles Table */}
          <div className="space-y-3">
            <div>
              <h3 className="text-base font-semibold text-foreground flex items-center gap-2">
                <ShieldCheck className="h-4 w-4 text-foreground" />
                Approved Component Profiles
              </h3>
              <p className="text-xs text-muted-foreground">
                Read-only view of workload specifications defined in the
                catalog. Only components flagged as overridable can be
                substituted.
              </p>
            </div>

            <div className="border border-border rounded-xl bg-card overflow-hidden shadow-2xs">
              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs">
                  <thead className="bg-muted/60 text-muted-foreground uppercase text-[10px] tracking-wider border-b border-border">
                    <tr>
                      <th className="py-3 px-4 font-semibold">Component ID</th>
                      <th className="py-3 px-4 font-semibold">Status / Mode</th>
                      <th className="py-3 px-4 font-semibold">
                        Protocol & Port
                      </th>
                      <th className="py-3 px-4 font-semibold">Health Path</th>
                      <th className="py-3 px-4 font-semibold">
                        Repository Provenance
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border font-mono">
                    {components
                      .filter(
                        (c) => !projectFilter || c.project === projectFilter,
                      )
                      .map((comp) => (
                        <tr
                          key={`${comp.project}/${comp.id}`}
                          className="hover:bg-muted/30 transition-colors"
                        >
                          <td className="py-3 px-4 font-semibold text-foreground flex items-center gap-2">
                            <span className="h-2 w-2 rounded-full bg-primary" />
                            {comp.project}/{comp.id}
                          </td>
                          <td className="py-3 px-4">
                            {comp.overridable ? (
                              <span className="inline-flex items-center gap-1 text-emerald-800 bg-emerald-50 border border-emerald-200/80 dark:text-emerald-300 dark:bg-emerald-950/40 dark:border-emerald-800/60 px-2 py-0.5 rounded-full text-[11px] font-sans font-medium">
                                <CheckCircle2 className="h-3 w-3 text-emerald-600 dark:text-emerald-400" />{" "}
                                Overridable
                              </span>
                            ) : (
                              <span className="inline-flex items-center gap-1 text-muted-foreground bg-muted border border-border px-2 py-0.5 rounded-full text-[11px] font-sans font-medium">
                                <Lock className="h-3 w-3 text-muted-foreground" />{" "}
                                Locked
                              </span>
                            )}
                          </td>
                          <td className="py-3 px-4 text-muted-foreground">
                            {comp.protocol.toUpperCase()}:{comp.port}
                          </td>
                          <td className="py-3 px-4 text-muted-foreground">
                            {comp.health_path}
                          </td>
                          <td className="py-3 px-4 text-muted-foreground">
                            <span className="flex items-center gap-1">
                              <GitBranch className="h-3 w-3 text-muted-foreground" />
                              {comp.repository || "None registered"}
                            </span>
                          </td>
                        </tr>
                      ))}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
