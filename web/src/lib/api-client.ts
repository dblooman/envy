import { csrf } from "./auth";
import type {
  GitHubStatus,
  GitHubInstallation,
  GitHubRepository,
  PreviewPolicy,
  PRPreview,
} from "../types/github";
import {
  CatalogManifest,
  CatalogReport,
  PreviewSelection,
  PreviewReport,
  PreviewProfile,
  PreviewApproval,
  VerificationEvidence,
  ObservabilityLink,
  SourceRepository,
  GitCommit,
  GitBranch,
  GitPage,
  RevisionResolution,
  Composition,
  FrontendBindingView,
  Project,
  Baseline,
  Component,
  PageResponse,
  CreateCompositionRequest,
  UpdateCompositionRequest,
  ApiError,
  ComponentLogs,
  LifecycleEvent,
  Session,
  Installation,
  Activity,
  CompositionRevision,
  Recipe,
  RecreateRecipeResult,
  FrontendResolution,
  PreviewPlan,
  OnboardingDraft,
} from "../types/api";

export class ApiRequestError extends Error {
  constructor(
    message: string,
    public status: number,
    public code?: string,
  ) {
    super(message);
  }
}

export class EnvyApiClient {
  verification(id: string, after = "", signal?: AbortSignal) {
    return this.request<PageResponse<VerificationEvidence>>(
      `/v1/compositions/${encodeURIComponent(id)}/verification?${new URLSearchParams({ after, limit: "20" })}`,
      { signal },
    );
  }
  observability(id: string, component = "", signal?: AbortSignal) {
    return this.request<{ items: ObservabilityLink[] }>(
      `/v1/compositions/${encodeURIComponent(id)}/observability?${new URLSearchParams({ component })}`,
      { signal },
    );
  }

  validateCatalog(manifest: CatalogManifest) {
    return this.request<CatalogReport>("/v1/catalog/validate", {
      method: "POST",
      body: JSON.stringify(manifest),
    });
  }
  planComposition(req: CreateCompositionRequest) {
    return this.request<PreviewPlan>("/v1/compositions/plan", {
      method: "POST",
      body: JSON.stringify(req),
    });
  }
  getOnboardingDraft(project: string) {
    return this.request<OnboardingDraft>(
      `/v1/projects/${encodeURIComponent(project)}/onboarding-draft`,
    );
  }
  saveOnboardingDraft(draft: OnboardingDraft) {
    return this.request<OnboardingDraft>(
      `/v1/projects/${encodeURIComponent(draft.project)}/onboarding-draft`,
      { method: "PUT", body: JSON.stringify(draft) },
    );
  }
  deleteOnboardingDraft(project: string, revision: number) {
    return this.request<void>(
      `/v1/projects/${encodeURIComponent(project)}/onboarding-draft?revision=${revision}`,
      { method: "DELETE" },
    );
  }
  applyCatalog(manifest: CatalogManifest) {
    return this.request<CatalogReport>("/v1/catalog/apply", {
      method: "POST",
      body: JSON.stringify(manifest),
    });
  }
  private previewProfilePath(
    project: string,
    baseline: string,
    component: string,
  ) {
    return `/v1/projects/${encodeURIComponent(project)}/baselines/${encodeURIComponent(baseline)}/components/${encodeURIComponent(component)}/preview-profile`;
  }
  discoverPreviewProfile(
    project: string,
    baseline: string,
    component: string,
    selection: PreviewSelection,
  ) {
    return this.request<PreviewReport>(
      `${this.previewProfilePath(project, baseline, component)}/discover`,
      { method: "POST", body: JSON.stringify(selection) },
    );
  }
  inspectPreviewProfile(project: string, baseline: string, component: string) {
    return this.request<PreviewProfile>(
      this.previewProfilePath(project, baseline, component),
    );
  }
  approvePreviewProfile(
    project: string,
    baseline: string,
    component: string,
    approval: PreviewApproval,
  ) {
    return this.request<PreviewProfile>(
      `${this.previewProfilePath(project, baseline, component)}/approve`,
      { method: "POST", body: JSON.stringify(approval) },
    );
  }

  githubStatus() {
    return this.request<GitHubStatus>("/v1/github/status");
  }
  githubInstallations(page = 1) {
    return this.request<GitPage<GitHubInstallation>>(
      `/v1/github/installations?page=${page}`,
    );
  }
  githubRepositories(id: number, page = 1) {
    return this.request<GitPage<GitHubRepository>>(
      `/v1/github/installations/${id}/repositories?page=${page}`,
    );
  }
  previewPolicies() {
    return this.request<{ items: PreviewPolicy[] }>(
      "/v1/github/preview-policies",
    );
  }
  savePreviewPolicy(p: PreviewPolicy) {
    return this.request<PreviewPolicy>(
      `${this.sourcePath(p.project, p.repository)}/preview-policy`,
      { method: "PUT", body: JSON.stringify(p) },
    );
  }
  prPreviews(after = "") {
    return this.request<PageResponse<PRPreview>>(
      `/v1/github/previews?limit=100&after=${encodeURIComponent(after)}`,
    );
  }
  prPreview(id: string) {
    return this.request<PRPreview>(
      `/v1/github/previews/${encodeURIComponent(id)}`,
    );
  }
  controlPRPreview(id: string, action: "stop" | "restart") {
    return this.request<PRPreview>(
      `/v1/github/previews/${encodeURIComponent(id)}/${action}`,
      { method: "POST" },
    );
  }
  private async request<T>(
    endpoint: string,
    options: RequestInit = {},
  ): Promise<T> {
    const headers = new Headers(options.headers || {});
    headers.set("Accept", "application/json");
    headers.set("X-Envy-Channel", "web");
    if (options.method && !["GET", "HEAD", "OPTIONS"].includes(options.method))
      headers.set("X-CSRF-Token", await csrf());
    if (options.body && typeof options.body === "string") {
      headers.set("Content-Type", "application/json");
    }

    const fullUrl = endpoint;
    let res: Response;
    try {
      res = await fetch(fullUrl, {
        ...options,
        headers,
        credentials: "same-origin",
      });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Network error";
      throw new Error(`Failed to connect to ${fullUrl}: ${msg}`);
    }

    if (res.status === 401)
      window.dispatchEvent(new Event("envy-unauthorized"));
    if (!res.ok) {
      let errData: { error?: ApiError } | null = null;
      try {
        errData = await res.json();
      } catch {
        // ignore parse error
      }

      if (errData?.error) {
        throw new ApiRequestError(
          `[${errData.error.code}] ${errData.error.message}`,
          res.status,
          errData.error.code,
        );
      }
      throw new ApiRequestError(
        `HTTP ${res.status}: ${res.statusText}`,
        res.status,
      );
    }

    if (res.status === 204) {
      return {} as T;
    }

    return (await res.json()) as T;
  }

  async listSourceRepositories(project: string): Promise<SourceRepository[]> {
    return this.catalogPages<SourceRepository>(
      `/v1/projects/${encodeURIComponent(project)}/repositories`,
    );
  }
  async registerSourceRepository(
    repository: SourceRepository,
  ): Promise<SourceRepository> {
    return this.request(
      `/v1/projects/${encodeURIComponent(repository.project)}/repositories`,
      { method: "POST", body: JSON.stringify(repository) },
    );
  }
  async enableSourceRepository(
    project: string,
    repository: string,
    enabled: boolean,
  ): Promise<SourceRepository> {
    return this.request(this.sourcePath(project, repository), {
      method: "PATCH",
      body: JSON.stringify({ enabled }),
    });
  }
  private sourcePath(project: string, repository: string) {
    return `/v1/projects/${encodeURIComponent(project)}/repositories/${encodeURIComponent(repository)}`;
  }
  async sourceBranches(
    project: string,
    repository: string,
    page = 1,
  ): Promise<GitPage<GitBranch>> {
    return this.request(
      `${this.sourcePath(project, repository)}/branches?page=${page}`,
    );
  }
  async sourceCommits(
    project: string,
    repository: string,
    branch: string,
    page = 1,
  ): Promise<GitPage<GitCommit>> {
    return this.request(
      `${this.sourcePath(project, repository)}/commits?${new URLSearchParams({ branch, page: String(page) })}`,
    );
  }
  async resolveRevision(
    project: string,
    repository: string,
    component: string,
    ref: string,
    after = "",
  ): Promise<RevisionResolution> {
    return this.request(
      `${this.sourcePath(project, repository)}/resolve?${new URLSearchParams({ component, ref, after, limit: "100" })}`,
    );
  }

  async listFrontendBindings(
    id: string,
    signal?: AbortSignal,
  ): Promise<PageResponse<FrontendBindingView>> {
    return this.request(
      `/v1/compositions/${encodeURIComponent(id)}/frontend-bindings?limit=100`,
      { signal },
    );
  }

  async session(): Promise<Session> {
    return this.request<Session>("/v1/session");
  }
  async installation(): Promise<Installation> {
    return this.request<Installation>("/v1/installation");
  }
  async listActivity(
    query: Record<string, string> = {},
    signal?: AbortSignal,
  ): Promise<PageResponse<Activity>> {
    return this.request(
      `/v1/activity?${new URLSearchParams({ limit: "50", ...query })}`,
      { signal },
    );
  }
  async listRevisions(
    id: string,
    signal?: AbortSignal,
    after = "",
  ): Promise<PageResponse<CompositionRevision>> {
    return this.request(
      `/v1/compositions/${encodeURIComponent(id)}/revisions?${new URLSearchParams({ limit: "100", after })}`,
      { signal },
    );
  }
  async exportRecipe(
    composition: string,
    frontends: { name: string; revision: string }[] = [],
  ): Promise<Recipe> {
    return this.request("/v1/recipes/export", {
      method: "POST",
      body: JSON.stringify({ composition, frontends }),
    });
  }
  async validateRecipe(
    recipe: Recipe,
  ): Promise<{ valid: boolean; recipe: Recipe }> {
    return this.request("/v1/recipes/validate", {
      method: "POST",
      body: JSON.stringify(recipe),
    });
  }
  async recreateRecipe(
    recipe: Recipe,
    name: string,
    idempotencyKey: string,
  ): Promise<RecreateRecipeResult> {
    return this.request("/v1/recipes/recreate", {
      method: "POST",
      body: JSON.stringify({ recipe, name, idempotency_key: idempotencyKey }),
    });
  }
  async bindFrontend(
    project: string,
    frontend: string,
    revision: string,
    composition: string,
    repository: string,
  ): Promise<FrontendBindingView> {
    return this.request(
      `/v1/projects/${encodeURIComponent(project)}/frontend-bindings/${encodeURIComponent(frontend)}/${encodeURIComponent(revision)}`,
      { method: "PUT", body: JSON.stringify({ composition, repository }) },
    );
  }
  async resolveFrontend(
    project: string,
    frontend: string,
    revision: string,
  ): Promise<FrontendResolution> {
    return this.request(
      `/v1/projects/${encodeURIComponent(project)}/frontend-bindings/${encodeURIComponent(frontend)}/${encodeURIComponent(revision)}/resolve`,
    );
  }
  async publishFrontend(
    project: string,
    frontend: string,
    revision: string,
    expectedVersion: number,
    url: string,
  ): Promise<FrontendBindingView> {
    return this.request(
      `/v1/projects/${encodeURIComponent(project)}/frontend-bindings/${encodeURIComponent(frontend)}/${encodeURIComponent(revision)}/deployment`,
      {
        method: "POST",
        body: JSON.stringify({ expected_version: expectedVersion, url }),
      },
    );
  }
  async checkFrontend(
    project: string,
    frontend: string,
    revision: string,
    expectedVersion: number,
    compositionGeneration: number,
    status: "passed" | "failed",
    message: string,
  ): Promise<FrontendBindingView> {
    return this.request(
      `/v1/projects/${encodeURIComponent(project)}/frontend-bindings/${encodeURIComponent(frontend)}/${encodeURIComponent(revision)}/check`,
      {
        method: "POST",
        body: JSON.stringify({
          expected_version: expectedVersion,
          composition_generation: compositionGeneration,
          status,
          message,
        }),
      },
    );
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
      const result = await this.request<PageResponse<T>>(
        `${path}?limit=100&after=${encodeURIComponent(after)}`,
      );
      items.push(...result.items);
      if (!result.next_cursor) return items;
      if (result.next_cursor === after)
        throw new Error("Catalog pagination did not advance");
      after = result.next_cursor;
    }
    throw new Error("Catalog exceeds the frontend's 1000-entry display limit");
  }
  async listProjects(): Promise<Project[]> {
    return this.catalogPages<Project>("/v1/projects");
  }
  async listBaselines(project = "demo"): Promise<Baseline[]> {
    return this.catalogPages<Baseline>(
      `/v1/projects/${encodeURIComponent(project)}/baselines`,
    );
  }
  async listComponents(project = "demo"): Promise<Component[]> {
    return this.catalogPages<Component>(
      `/v1/projects/${encodeURIComponent(project)}/components`,
    );
  }
  async registerProject(project: Project): Promise<Project> {
    return this.request<Project>("/v1/projects", {
      method: "POST",
      body: JSON.stringify(project),
    });
  }
  async registerComponent(component: Component): Promise<Component> {
    return this.request<Component>(
      `/v1/projects/${encodeURIComponent(component.project)}/components`,
      { method: "POST", body: JSON.stringify(component) },
    );
  }
  async registerBaseline(baseline: Baseline): Promise<Baseline> {
    return this.request<Baseline>(
      `/v1/projects/${encodeURIComponent(baseline.project)}/baselines`,
      { method: "POST", body: JSON.stringify(baseline) },
    );
  }

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
    container = "",
    previous = false,
    options: {
      tail_lines?: number;
      max_bytes?: number;
      since_seconds?: number;
    } = {},
  ): Promise<ComponentLogs> {
    const query = new URLSearchParams({
      tail_lines: String(options.tail_lines || 200),
      max_bytes: String(options.max_bytes || 65536),
    });
    if (options.since_seconds)
      query.set("since_seconds", String(options.since_seconds));
    if (container.trim()) query.set("container", container.trim());
    if (previous) query.set("previous", "true");
    return this.request<ComponentLogs>(
      `/v1/compositions/${encodeURIComponent(id)}/components/${encodeURIComponent(component)}/logs?${query}`,
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
