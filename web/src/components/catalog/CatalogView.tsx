import {
  Boxes,
  Server,
  GitBranch,
  CheckCircle2,
  ShieldCheck,
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

export function CatalogView() {
  const { projects, baselines, components } = useEnvyApi();

  return (
    <div className="space-y-8">
      <CatalogRegistration />
      {/* Overview Cards */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {projects.map((proj) => (
          <Card key={proj.id} className="border-border bg-card/60">
            <CardHeader className="pb-3">
              <div className="flex items-center justify-between">
                <CardTitle className="text-base flex items-center gap-2">
                  <Boxes className="h-4 w-4 text-primary" />
                  {proj.name}
                </CardTitle>
                <span className="font-mono text-xs px-2 py-0.5 rounded bg-primary/10 text-primary border border-primary/20">
                  {proj.id}
                </span>
              </div>
              <CardDescription>
                Logical project scope for ephemeral compositions and baselines.
              </CardDescription>
            </CardHeader>
            <CardContent className="text-xs text-muted-foreground space-y-1">
              <div className="flex justify-between">
                <span>Total Components:</span>
                <span className="font-mono text-foreground">
                  {components.filter(c => c.project === proj.id).length}
                </span>
              </div>
              <div className="flex justify-between">
                <span>Active Baselines:</span>
                <span className="font-mono text-foreground">
                  {baselines.filter(b => b.project === proj.id).length}
                </span>
              </div>
            </CardContent>
          </Card>
        ))}

        {baselines.map((baseline) => (
          <Card
            key={`${baseline.project}/${baseline.project}/{baseline.id}`}
            className="border-border bg-card/60 md:col-span-2"
          >
            <CardHeader className="pb-3">
              <div className="flex items-center justify-between">
                <CardTitle className="text-base flex items-center gap-2">
                  <Server className="h-4 w-4 text-emerald-400" />
                  Shared Baseline:{" "}
                  <span className="text-foreground capitalize">
                    {baseline.project}/{baseline.id}
                  </span>
                </CardTitle>
                <Badge variant="outline" className="font-mono text-[11px]">
                  Revision: {baseline.revision}
                </Badge>
              </div>
              <CardDescription>
                Inherited baseline environment. State, databases, caches, and
                baseline pods remain shared.
              </CardDescription>
            </CardHeader>
            <CardContent className="text-xs space-y-2">
              <div className="flex items-center gap-2 bg-background/60 p-2 rounded border border-border/60">
                <span className="text-muted-foreground">Endpoint:</span>
                <a
                  href={baseline.endpoint}
                  target="_blank"
                  rel="noreferrer"
                  className="font-mono text-primary hover:underline flex items-center gap-1"
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
            <ShieldCheck className="h-4 w-4 text-primary" />
            Approved Component Profiles
          </h3>
          <p className="text-xs text-muted-foreground">
            Workload specifications defined in the catalog. Only components
            flagged as overridable can be substituted.
          </p>
        </div>

        <div className="border border-border rounded-xl bg-card/60 overflow-hidden shadow-sm">
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead className="bg-secondary/60 text-muted-foreground uppercase text-[10px] tracking-wider border-b border-border">
                <tr>
                  <th className="py-3 px-4 font-semibold">Component ID</th>
                  <th className="py-3 px-4 font-semibold">Status / Mode</th>
                  <th className="py-3 px-4 font-semibold">Protocol & Port</th>
                  <th className="py-3 px-4 font-semibold">Health Path</th>
                  <th className="py-3 px-4 font-semibold">
                    Repository Provenance
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border/60 font-mono">
                {components.map((comp) => (
                  <tr
                    key={`${comp.project}/${comp.project}/{comp.id}`}
                    className="hover:bg-accent/40 transition-colors"
                  >
                    <td className="py-3 px-4 font-semibold text-foreground flex items-center gap-2">
                      <span className="h-2 w-2 rounded-full bg-blue-400" />
                      {comp.project}/{comp.id}
                    </td>
                    <td className="py-3 px-4">
                      {comp.overridable ? (
                        <span className="inline-flex items-center gap-1 text-emerald-400 bg-emerald-950/60 border border-emerald-800/80 px-2 py-0.5 rounded-full text-[11px] font-sans font-medium">
                          <CheckCircle2 className="h-3 w-3" /> Overridable
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 text-zinc-400 bg-zinc-800/60 border border-zinc-700/60 px-2 py-0.5 rounded-full text-[11px] font-sans font-medium">
                          Shared Baseline
                        </span>
                      )}
                    </td>
                    <td className="py-3 px-4 text-muted-foreground font-mono">
                      {comp.protocol.toUpperCase()} : {comp.port}
                    </td>
                    <td className="py-3 px-4 text-muted-foreground font-mono">
                      {comp.health_path || "/healthz"}
                    </td>
                    <td className="py-3 px-4 font-sans text-muted-foreground truncate max-w-xs">
                      {comp.repository ? (
                        <span className="flex items-center gap-1 text-xs">
                          <GitBranch className="h-3.5 w-3.5 text-muted-foreground" />
                          {comp.repository}
                        </span>
                      ) : (
                        <span className="text-zinc-500 italic">
                          None registered
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  );
}
