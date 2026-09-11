import { useMemo, useState } from "react";
import { ArrowRight, Network, Server, Waypoints } from "lucide-react";
import { useEnvyApi } from "../../context/ApiContext";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
} from "../ui/card";

export function TopologyView() {
  const { compositions, baselines } = useEnvyApi();
  const [selected, setSelected] = useState("");
  const composition = compositions.find((c) => c.id === selected);
  const baseline = useMemo(
    () =>
      composition
        ? baselines.find(
            (b) =>
              b.project === composition.project &&
              b.id === composition.baseline,
          )
        : baselines[0],
    [composition, baselines],
  );
  return (
    <div className="space-y-5">
      <div className="rounded-xl border border-border bg-card p-4">
        <label className="text-sm font-medium">
          Configured routing view
          <select
            value={selected}
            onChange={(e) => setSelected(e.target.value)}
            className="mt-2 h-9 w-full rounded-md border border-input bg-background px-3"
          >
            <option value="">Baseline configuration</option>
            {compositions
              .filter((c) => c.phase !== "destroyed")
              .map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name} · {c.project}/{c.baseline}
                </option>
              ))}
          </select>
        </label>
      </div>
      {!baseline ? (
        <p className="rounded-xl border border-dashed border-border p-10 text-center text-muted-foreground">
          Register a baseline to inspect its routing configuration.
        </p>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Network className="h-4 w-4" />
              Registered destinations
            </CardTitle>
            <CardDescription>
              This is Envy’s configured routing intent. It does not claim a
              measured service-call graph or current proxy health.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex flex-col items-stretch gap-3 lg:flex-row lg:items-center">
              <div className="rounded-lg border border-border p-4">
                <Waypoints className="mb-2 h-5 w-5" />
                <strong className="block">{baseline.endpoint}</strong>
                <span className="text-sm text-muted-foreground">
                  Ingress gateway:{" "}
                  {baseline.routing?.gateway || "registered externally"}
                </span>
              </div>
              <ArrowRight className="hidden h-5 w-5 text-muted-foreground lg:block" />
              <div className="grid flex-1 gap-2 sm:grid-cols-2">
                {Object.entries(baseline.components).map(([name, binding]) => {
                  const override = composition?.overrides[name];
                  return (
                    <div
                      key={name}
                      className={`rounded-lg border p-3 ${override ? "border-emerald-400 bg-emerald-50 dark:bg-emerald-950/30" : "border-border"}`}
                    >
                      <div className="flex items-center gap-2">
                        <Server className="h-4 w-4" />
                        <strong>{name}</strong>
                      </div>
                      <p className="mt-1 break-all font-mono text-xs">
                        {override?.image || binding.image}
                      </p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {override
                          ? "Composition-owned override service"
                          : "Inherited baseline service"}
                      </p>
                    </div>
                  );
                })}
              </div>
            </div>
            {composition && (
              <div className="rounded-lg border border-border bg-muted/40 p-3 text-sm">
                <strong>Selection context</strong>
                <p className="mt-1 font-mono text-xs">
                  baggage: composition={composition.id}
                </p>
                <p className="mt-2 text-muted-foreground">
                  Ingress normalizes this member. Every application hop must
                  retain the incoming request context and inject it on outbound
                  calls. An unhealthy override remains explicitly routed and
                  fails visibly.
                </p>
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
