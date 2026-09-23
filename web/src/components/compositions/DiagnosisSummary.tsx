import { useEffect, useState } from "react";
import { useEnvyApi } from "../../context/ApiContext";
import { apiClient } from "../../lib/api-client";
import type { Composition, Diagnosis } from "../../types/api";

export function DiagnosisSummary({
  composition,
}: {
  composition: Composition;
}) {
  const { isDemoMode } = useEnvyApi();
  const [diagnosis, setDiagnosis] = useState<Diagnosis | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    setDiagnosis(null);
    setError("");
    if (isDemoMode) return () => controller.abort();

    const load = () => {
      apiClient
        .diagnosis(composition.id, controller.signal)
        .then((result) => {
          if (!controller.signal.aborted) {
            setDiagnosis(result);
            setError("");
          }
        })
        .catch(() => {
          if (!controller.signal.aborted)
            setError(
              "Diagnosis could not be loaded. Inspect conditions and events.",
            );
        });
    };
    load();
    const interval = window.setInterval(load, 30_000);
    return () => {
      window.clearInterval(interval);
      controller.abort();
    };
  }, [
    composition.id,
    composition.generation,
    composition.updated_at,
    isDemoMode,
  ]);
  return (
    <section
      className="envy-panel space-y-3"
      aria-label="Composition diagnosis"
    >
      <h2>What is blocking this preview?</h2>
      {isDemoMode ? (
        <p>Simulated preview · live diagnosis unavailable.</p>
      ) : error ? (
        <p role="status">{error}</p>
      ) : !diagnosis ? (
        <p>Loading observed conditions…</p>
      ) : (
        <>
          <p>
            {diagnosis.state === "blocked"
              ? `${diagnosis.blockers.length} observed blocker${diagnosis.blockers.length === 1 ? "" : "s"}.`
              : diagnosis.state === "cleanup"
                ? "Cleanup status is separate from serving readiness."
                : `Composition status: ${diagnosis.state}.`}{" "}
            Verification: {diagnosis.verification.replaceAll("_", " ")}.
          </p>
          {diagnosis.blockers.length === 0 && diagnosis.state === "pending" && (
            <p>
              Waiting for more observations; no specific blocker is known yet.
            </p>
          )}
          {diagnosis.blockers.length > 1 && (
            <p>
              These observations may be independent. Their order does not
              establish a root cause.
            </p>
          )}
          <ul className="space-y-2">
            {diagnosis.blockers.map((finding) => (
              <li
                key={`${finding.code}/${finding.scope}`}
                className="rounded border border-border p-2"
              >
                <strong>
                  {finding.scope.replaceAll("/", " · ")}: {finding.message}
                </strong>
                <p>{finding.next_step}</p>
                {finding.depends_on?.length ? (
                  <p>Declared dependencies: {finding.depends_on.join(", ")}</p>
                ) : null}
                <small>
                  Observed {new Date(finding.observed_at).toLocaleString()}
                </small>
              </li>
            ))}
          </ul>
          {diagnosis.notes.map((finding) => (
            <p key={`${finding.code}/${finding.scope}`}>
              {finding.message} {finding.next_step}
            </p>
          ))}
        </>
      )}
    </section>
  );
}
