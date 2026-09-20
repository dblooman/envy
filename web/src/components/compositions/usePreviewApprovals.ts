import { useEffect, useState } from "react";
import { apiClient, ApiRequestError } from "../../lib/api-client";
import type { Component } from "../../types/api";

export function usePreviewApprovals(
  project: string,
  baseline: string,
  components: Component[],
  enabled: boolean,
): {
  revisions: Record<string, number>;
  errors: Record<string, string>;
  loading: boolean;
  reload: () => void;
} {
  const ids = JSON.stringify(
    components
      .filter((c) => c.profile === "deployment")
      .map((c) => c.id)
      .sort(),
  );
  const scope = JSON.stringify([project, baseline, ids, enabled]);
  const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<{
    scope: string;
    revisions: Record<string, number>;
    errors: Record<string, string>;
    loading: boolean;
  }>({ scope: "", revisions: {}, errors: {}, loading: true });
  useEffect(() => {
    let cancelled = false;
    const componentIds: string[] = JSON.parse(ids);
    if (!enabled || !componentIds.length) return;
    setState({ scope, revisions: {}, errors: {}, loading: true });
    void Promise.all(
      componentIds.map(async (id) => {
        try {
          return {
            id,
            revision: (
              await apiClient.inspectPreviewProfile(project, baseline, id)
            ).revision,
          };
        } catch (error) {
          return {
            id,
            error:
              error instanceof ApiRequestError && error.status === 404
                ? "Prepare this service before overriding it."
                : `Could not check approval: ${error instanceof Error ? error.message : String(error)}`,
          };
        }
      }),
    ).then((results) => {
      if (cancelled) return;
      setState({
        scope,
        revisions: Object.fromEntries(
          results.filter((r) => r.revision).map((r) => [r.id, r.revision!]),
        ),
        errors: Object.fromEntries(
          results.filter((r) => r.error).map((r) => [r.id, r.error!]),
        ),
        loading: false,
      });
    });
    return () => {
      cancelled = true;
    };
  }, [project, baseline, ids, enabled, scope, attempt]);
  if (!enabled || ids === "[]")
    return {
      revisions: {},
      errors: {},
      loading: false,
      reload: () => setAttempt((n) => n + 1),
    };
  return {
    ...(state.scope === scope
      ? state
      : { revisions: {}, errors: {}, loading: true }),
    reload: () => setAttempt((n) => n + 1),
  };
}
