import React, {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  useRef,
} from "react";
import {
  Composition,
  Project,
  Baseline,
  Component,
  CreateCompositionRequest,
  UpdateCompositionRequest,
} from "../types/api";
import { apiClient } from "../lib/api-client";
import {
  INITIAL_MOCK_COMPOSITIONS,
  MOCK_PROJECTS,
  MOCK_BASELINES,
  MOCK_COMPONENTS,
} from "../lib/mock-data";

export type ServerStatus = "connected" | "disconnected" | "connecting" | "demo";

interface ApiContextType {
  serverUrl: string;
  setServerUrl: (url: string) => void;
  token: string;
  setToken: (token: string) => void;
  isDemoMode: boolean;
  setDemoMode: (enabled: boolean) => void;
  serverStatus: ServerStatus;
  compositions: Composition[];
  projects: Project[];
  baselines: Baseline[];
  components: Component[];
  loading: boolean;
  error: string | null;
  refreshAll: () => Promise<void>;
  createComposition: (
    req: CreateCompositionRequest,
    idempotencyKey?: string,
  ) => Promise<Composition>;
  updateComposition: (
    id: string,
    req: UpdateCompositionRequest,
  ) => Promise<Composition>;
  destroyComposition: (id: string) => Promise<Composition>;
  testConnection: () => Promise<{ ok: boolean; message: string }>;
}

const ApiContext = createContext<ApiContextType | undefined>(undefined);

export function ApiProvider({ children }: { children: React.ReactNode }) {
  const [serverUrl, setServerUrlState] = useState<string>(() => {
    return localStorage.getItem("envy_server_url") || "";
  });
  const [token, setTokenState] = useState<string>(() => {
    return localStorage.getItem("envy_api_token") || "";
  });
  const [isDemoMode, setDemoModeState] = useState<boolean>(() => {
    const saved = localStorage.getItem("envy_demo_mode");
    return saved !== null ? saved === "true" : true; // Default to demo mode initially so UI immediately works
  });

  const [serverStatus, setServerStatus] = useState<ServerStatus>("connecting");
  const [compositions, setCompositions] = useState<Composition[]>(
    INITIAL_MOCK_COMPOSITIONS,
  );
  const [projects, setProjects] = useState<Project[]>(MOCK_PROJECTS);
  const [baselines, setBaselines] = useState<Baseline[]>(MOCK_BASELINES);
  const [components, setComponents] = useState<Component[]>(MOCK_COMPONENTS);
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  const pollingTimerRef = useRef<number | null>(null);

  const setServerUrl = (url: string) => {
    setServerUrlState(url);
    localStorage.setItem("envy_server_url", url);
    apiClient.setBaseUrl(url);
  };

  const setToken = (newToken: string) => {
    setTokenState(newToken);
    localStorage.setItem("envy_api_token", newToken);
    apiClient.setToken(newToken);
  };

  const setDemoMode = (enabled: boolean) => {
    setDemoModeState(enabled);
    localStorage.setItem("envy_demo_mode", enabled ? "true" : "false");
    if (enabled) {
      setServerStatus("demo");
      setCompositions(INITIAL_MOCK_COMPOSITIONS);
      setProjects(MOCK_PROJECTS);
      setBaselines(MOCK_BASELINES);
      setComponents(MOCK_COMPONENTS);
    }
  };

  // Initialize client settings
  useEffect(() => {
    apiClient.setBaseUrl(serverUrl);
    apiClient.setToken(token);
  }, [serverUrl, token]);

  const testConnection = useCallback(async (): Promise<{
    ok: boolean;
    message: string;
  }> => {
    try {
      const res = await apiClient.checkHealth();
      if (res.status === "ok") {
        try {
          await apiClient.listProjects();
          return {
            ok: true,
            message:
              "Connected to Envy Control Plane with authenticated access.",
          };
        } catch (authErr: unknown) {
          const authMsg =
            authErr instanceof Error ? authErr.message : String(authErr);
          if (authMsg.includes("unauthorized") || authMsg.includes("401")) {
            return {
              ok: false,
              message:
                "Server online, but Bearer Token is invalid or missing. Please set your API token.",
            };
          }
          return { ok: false, message: `Server error: ${authMsg}` };
        }
      }
      return { ok: false, message: `Unexpected status: ${res.status}` };
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      return { ok: false, message: msg };
    }
  }, []);

  const refreshAll = useCallback(async () => {
    if (isDemoMode) {
      setServerStatus("demo");
      return;
    }

    setLoading(true);
    setError(null);
    try {
      const [pList, cList] = await Promise.all([
        apiClient.listProjects().catch(() => []),
        apiClient.listCompositions().catch(() => []),
      ]);

      setServerStatus("connected");
      setProjects(pList.length ? pList : MOCK_PROJECTS);
      setCompositions(cList);

      if (pList.length > 0) {
        const pId = pList[0].id;
        const [bList, compList] = await Promise.all([
          apiClient.listBaselines(pId).catch(() => []),
          apiClient.listComponents(pId).catch(() => []),
        ]);
        if (bList.length) setBaselines(bList);
        if (compList.length) setComponents(compList);
      }
    } catch (err: unknown) {
      setServerStatus("disconnected");
      const msg =
        err instanceof Error ? err.message : "Failed to fetch from Envy server";
      setError(msg);
    } finally {
      setLoading(false);
    }
  }, [isDemoMode]);

  // Periodic polling for active compositions or status updates
  useEffect(() => {
    const checkAndPoll = async () => {
      if (isDemoMode) {
        setServerStatus("demo");
        return;
      }

      const res = await testConnection();
      if (res.ok) {
        setServerStatus("connected");
        refreshAll();
      } else {
        setServerStatus("disconnected");
      }
    };

    checkAndPoll();

    pollingTimerRef.current = window.setInterval(() => {
      if (!isDemoMode && serverStatus === "connected") {
        apiClient
          .listCompositions()
          .then((items) => setCompositions(items))
          .catch(() => {});
      }
    }, 4000);

    return () => {
      if (pollingTimerRef.current) {
        clearInterval(pollingTimerRef.current);
      }
    };
  }, [isDemoMode, testConnection, refreshAll, serverStatus]);

  const createComposition = async (
    req: CreateCompositionRequest,
    idempotencyKey?: string,
  ): Promise<Composition> => {
    if (isDemoMode) {
      const newId = `01999999-demo-${Date.now().toString(16).slice(-12)}`;
      const newComp: Composition = {
        id: newId,
        project: req.project,
        baseline: req.baseline,
        baseline_revision: "rev-20260905-1a",
        name: req.name,
        generation: 1,
        observed_generation: 0,
        phase: "provisioning",
        overrides: req.overrides,
        components: {
          gateway: {
            source: "baseline",
            status: "healthy",
            image: "envy/gateway:v1",
            workload_id: "gateway-v1-baseline",
          },
          "service-a": {
            source: "baseline",
            status: "healthy",
            image: "envy/service-a:v1",
            workload_id: "service-a-v1-baseline",
          },
          "service-b": {
            source: "override",
            status: "container_creating",
            image: req.overrides["service-b"]?.image || "envy/service-b:v2",
            workload_id: `cmp-${newId.slice(0, 8)}-service-b`,
          },
        },
        conditions: [
          {
            type: "WorkloadsReady",
            status: false,
            message: "Creating pod override",
          },
          {
            type: "RoutesConfigured",
            status: true,
            message: "Istio VirtualService baggage rule created",
          },
          {
            type: "RouteVerified",
            status: false,
            message: "Synthetic HTTP probe pending",
          },
        ],
        latest_operation: {
          id: `op-${Date.now()}`,
          kind: "create",
          status: "running",
        },
        endpoints: {
          public: {
            url: `http://cmp-${newId}.envy.localhost:8080`,
            ready: false,
          },
        },
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        expires_at: new Date(Date.now() + 8 * 3600 * 1000).toISOString(),
      };

      setCompositions((prev) => [newComp, ...prev]);

      // Simulate completion after 3 seconds in demo mode
      setTimeout(() => {
        setCompositions((prev) =>
          prev.map((c) => {
            if (c.id === newId) {
              return {
                ...c,
                phase: "ready",
                observed_generation: 1,
                endpoints: {
                  public: {
                    ...c.endpoints.public,
                    ready: true,
                  },
                },
                conditions: [
                  {
                    type: "WorkloadsReady",
                    status: true,
                    message: "All override pods running and healthy",
                  },
                  {
                    type: "RoutesConfigured",
                    status: true,
                    message: "VirtualService routing active",
                  },
                  {
                    type: "RouteVerified",
                    status: true,
                    message: "Verified 200 OK via ingress",
                  },
                ],
                latest_operation: {
                  ...c.latest_operation,
                  status: "completed",
                },
              };
            }
            return c;
          }),
        );
      }, 3000);

      return newComp;
    }

    const created = await apiClient.createComposition(req, idempotencyKey);
    setCompositions((prev) => [created, ...prev]);
    return created;
  };

  const updateComposition = async (
    id: string,
    req: UpdateCompositionRequest,
  ): Promise<Composition> => {
    if (isDemoMode) {
      let updatedComp: Composition | undefined;
      setCompositions((prev) =>
        prev.map((c) => {
          if (c.id === id) {
            const nextGen = c.generation + 1;
            updatedComp = {
              ...c,
              generation: nextGen,
              phase: "updating",
              overrides: req.overrides,
              updated_at: new Date().toISOString(),
              latest_operation: {
                id: `op-${Date.now()}`,
                kind: "update",
                status: "running",
              },
            };
            return updatedComp;
          }
          return c;
        }),
      );

      setTimeout(() => {
        setCompositions((prev) =>
          prev.map((c) => {
            if (c.id === id) {
              return {
                ...c,
                phase: "ready",
                observed_generation: c.generation,
                components: {
                  ...c.components,
                  "service-b": {
                    ...c.components["service-b"],
                    image:
                      req.overrides["service-b"]?.image ||
                      c.components["service-b"].image,
                  },
                },
                latest_operation: {
                  ...c.latest_operation,
                  status: "completed",
                },
              };
            }
            return c;
          }),
        );
      }, 2500);

      if (!updatedComp) throw new Error("Composition not found");
      return updatedComp;
    }

    const updated = await apiClient.updateComposition(id, req);
    setCompositions((prev) => prev.map((c) => (c.id === id ? updated : c)));
    return updated;
  };

  const destroyComposition = async (id: string): Promise<Composition> => {
    if (isDemoMode) {
      let destroyedComp: Composition | undefined;
      setCompositions((prev) =>
        prev.map((c) => {
          if (c.id === id) {
            destroyedComp = {
              ...c,
              phase: "destroying",
              latest_operation: {
                id: `op-${Date.now()}`,
                kind: "destroy",
                status: "running",
              },
            };
            return destroyedComp;
          }
          return c;
        }),
      );

      setTimeout(() => {
        setCompositions((prev) =>
          prev.map((c) => {
            if (c.id === id) {
              return {
                ...c,
                phase: "destroyed",
                latest_operation: {
                  ...c.latest_operation,
                  status: "completed",
                },
              };
            }
            return c;
          }),
        );
      }, 2000);

      if (!destroyedComp) throw new Error("Composition not found");
      return destroyedComp;
    }

    const destroyed = await apiClient.destroyComposition(id);
    setCompositions((prev) => prev.map((c) => (c.id === id ? destroyed : c)));
    return destroyed;
  };

  return (
    <ApiContext.Provider
      value={{
        serverUrl,
        setServerUrl,
        token,
        setToken,
        isDemoMode,
        setDemoMode,
        serverStatus,
        compositions,
        projects,
        baselines,
        components,
        loading,
        error,
        refreshAll,
        createComposition,
        updateComposition,
        destroyComposition,
        testConnection,
      }}
    >
      {children}
    </ApiContext.Provider>
  );
}

export function useEnvyApi() {
  const context = useContext(ApiContext);
  if (!context) {
    throw new Error("useEnvyApi must be used within an ApiProvider");
  }
  return context;
}
