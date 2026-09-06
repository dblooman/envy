import {
  Composition,
  Project,
  Baseline,
  Component,
  PageResponse,
  CreateCompositionRequest,
  UpdateCompositionRequest,
  ApiError,
  ComponentLogs,
  LifecycleEvent,
} from "../types/api";

export class EnvyApiClient {
  private baseUrl: string;
  private token: string;

  constructor(baseUrl: string = "", token: string = "") {
    this.baseUrl = baseUrl.replace(/\/$/, "");
    this.token = token.trim();
  }

  setToken(token: string) {
    this.token = token.trim();
  }

  setBaseUrl(url: string) {
    this.baseUrl = url.replace(/\/$/, "");
  }

  private async request<T>(
    endpoint: string,
    options: RequestInit = {},
  ): Promise<T> {
    const headers = new Headers(options.headers || {});
    headers.set("Accept", "application/json");
    if (this.token) {
      headers.set("Authorization", `Bearer ${this.token}`);
    }
    if (options.body && typeof options.body === "string") {
      headers.set("Content-Type", "application/json");
    }

    const fullUrl = `${this.baseUrl}${endpoint}`;
    let res: Response;
    try {
      res = await fetch(fullUrl, {
        ...options,
        headers,
      });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Network error";
      throw new Error(`Failed to connect to ${fullUrl}: ${msg}`);
    }

    if (!res.ok) {
      let errData: { error?: ApiError } | null = null;
      try {
        errData = await res.json();
      } catch {
        // ignore parse error
      }

      if (errData?.error) {
        throw new Error(`[${errData.error.code}] ${errData.error.message}`);
      }
      throw new Error(`HTTP ${res.status}: ${res.statusText}`);
    }

    if (res.status === 204) {
      return {} as T;
    }

    return (await res.json()) as T;
  }

  async checkHealth(): Promise<{ status: string }> {
    return this.request<{ status: string }>("/healthz");
  }

  async checkReady(): Promise<{ status: string }> {
    return this.request<{ status: string }>("/readyz");
  }

  private async catalogPages<T>(path: string): Promise<T[]> {
    const items: T[] = [];
    let after = "";
    for (let page = 0; page < 10; page++) {
      const result = await this.request<PageResponse<T>>(`${path}?limit=100&after=${encodeURIComponent(after)}`);
      items.push(...result.items);
      if (!result.next_cursor) return items;
      if (result.next_cursor === after) throw new Error("Catalog pagination did not advance");
      after = result.next_cursor;
    }
    throw new Error("Catalog exceeds the frontend's 1000-entry display limit");
  }
  async listProjects(): Promise<Project[]> { return this.catalogPages<Project>("/v1/projects"); }
  async listBaselines(project = "demo"): Promise<Baseline[]> { return this.catalogPages<Baseline>(`/v1/projects/${encodeURIComponent(project)}/baselines`); }
  async listComponents(project = "demo"): Promise<Component[]> { return this.catalogPages<Component>(`/v1/projects/${encodeURIComponent(project)}/components`); }
  async registerProject(project: Project): Promise<Project> { return this.request<Project>("/v1/projects", { method: "POST", body: JSON.stringify(project) }); }
  async registerComponent(component: Component): Promise<Component> { return this.request<Component>(`/v1/projects/${encodeURIComponent(component.project)}/components`, { method: "POST", body: JSON.stringify(component) }); }
  async registerBaseline(baseline: Baseline): Promise<Baseline> { return this.request<Baseline>(`/v1/projects/${encodeURIComponent(baseline.project)}/baselines`, { method: "POST", body: JSON.stringify(baseline) }); }

  async listCompositions(project?: string): Promise<Composition[]> {
    const q = project ? `?project=${encodeURIComponent(project)}` : "";
    const res = await this.request<PageResponse<Composition>>(
      `/v1/compositions${q}`,
    );
    return res.items || [];
  }

  async getComposition(id: string): Promise<Composition> {
    return this.request<Composition>(
      `/v1/compositions/${encodeURIComponent(id)}`,
    );
  }

  async createComposition(
    req: CreateCompositionRequest,
    idempotencyKey?: string,
  ): Promise<Composition> {
    const headers: Record<string, string> = {};
    if (idempotencyKey) {
      headers["Idempotency-Key"] = idempotencyKey;
    }
    return this.request<Composition>("/v1/compositions", {
      method: "POST",
      headers,
      body: JSON.stringify(req),
    });
  }

  async updateComposition(
    id: string,
    req: UpdateCompositionRequest,
  ): Promise<Composition> {
    return this.request<Composition>(
      `/v1/compositions/${encodeURIComponent(id)}`,
      {
        method: "PATCH",
        body: JSON.stringify(req),
      },
    );
  }

  async getComponentLogs(
    id: string,
    component: string,
    signal?: AbortSignal,
  ): Promise<ComponentLogs> {
    return this.request<ComponentLogs>(
      `/v1/compositions/${encodeURIComponent(id)}/components/${encodeURIComponent(component)}/logs?tail_lines=200&max_bytes=65536`,
      { signal },
    );
  }

  async listCompositionEvents(
    id: string,
    after = "",
    signal?: AbortSignal,
  ): Promise<PageResponse<LifecycleEvent>> {
    const query = new URLSearchParams({ limit: "20", after });
    return this.request<PageResponse<LifecycleEvent>>(
      `/v1/compositions/${encodeURIComponent(id)}/events?${query}`,
      { signal },
    );
  }

  async destroyComposition(id: string): Promise<Composition> {
    return this.request<Composition>(
      `/v1/compositions/${encodeURIComponent(id)}`,
      {
        method: "DELETE",
      },
    );
  }
}

export const apiClient = new EnvyApiClient();
