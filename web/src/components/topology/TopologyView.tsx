import { useState } from "react";
import { Network, Server } from "lucide-react";
import { useEnvyApi } from "../../context/ApiContext";

export function TopologyView({
  baselineId,
  projectId,
}: { baselineId?: string; projectId?: string } = {}) {
  const { baselines } = useEnvyApi();
  const [selected, setSelected] = useState("");
  const baseline = baselineId
    ? baselines.find((b) => b.id === baselineId && b.project === projectId)
    : baselines.find((b) => `${b.project}/${b.id}` === selected) ||
      baselines[0];
  return (
    <div className="space-y-4">
      {!baselineId && (
        <label className="block text-sm">
          Baseline
          <select
            className="envy-input mt-2 w-full"
            value={baseline ? `${baseline.project}/${baseline.id}` : ""}
            onChange={(e) => setSelected(e.target.value)}
          >
            {baselines.map((b) => (
              <option
                key={`${b.project}/${b.id}`}
                value={`${b.project}/${b.id}`}
              >
                {b.project} / {b.id}
              </option>
            ))}
          </select>
        </label>
      )}
      {!baseline ? (
        <p className="envy-panel">
          Register a baseline to inspect its routing configuration.
        </p>
      ) : (
        <section className="envy-panel space-y-4">
          <h2 className="flex gap-2 items-center">
            <Network size={18} />
            Registered destinations
          </h2>
          <p className="text-muted-foreground">
            Configured routing intent, not a measured service-call graph or
            current proxy health. Inspect a preview to see its selected changes
            and verification evidence.
          </p>
          <dl className="envy-facts">
            <div>
              <dt>Endpoint</dt>
              <dd className="wrap-anywhere">{baseline.endpoint}</dd>
            </div>
            <div>
              <dt>Entry service</dt>
              <dd>{baseline.routing?.entry_component || "Not declared"}</dd>
            </div>
            <div>
              <dt>Gateway</dt>
              <dd>{baseline.routing?.gateway || "Registered externally"}</dd>
            </div>
          </dl>
          <div className="envy-service-list">
            {Object.entries(baseline.components).map(([name, binding]) => (
              <div className="envy-service-row" key={name}>
                <Server size={16} />
                <span>
                  <strong>{name}</strong>
                  <small>
                    {binding.service_host}:{binding.port}
                  </small>
                  <small>{binding.image}</small>
                </span>
                <span>Shared baseline</span>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}
