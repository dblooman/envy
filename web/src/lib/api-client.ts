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
} from '../types/api'

export class EnvyApiClient {
  private baseUrl: string
  private token: string

  constructor(baseUrl: string = '', token: string = '') {
    this.baseUrl = baseUrl.replace(/\/$/, '')
    this.token = token.trim()
  }

  setToken(token: string) {
    this.token = token.trim()
  }

  setBaseUrl(url: string) {
    this.baseUrl = url.replace(/\/$/, '')
  }

  private async request<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> {
    const headers = new Headers(options.headers || {})
    headers.set('Accept', 'application/json')
    if (this.token) {
      headers.set('Authorization', `Bearer ${this.token}`)
    }
    if (options.body && typeof options.body === 'string') {
      headers.set('Content-Type', 'application/json')
    }

    const fullUrl = `${this.baseUrl}${endpoint}`
    let res: Response
    try {
      res = await fetch(fullUrl, {
        ...options,
        headers,
      })
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Network error'
      throw new Error(`Failed to connect to ${fullUrl}: ${msg}`)
    }

    if (!res.ok) {
      let errData: { error?: ApiError } | null = null
      try {
        errData = await res.json()
      } catch {
        // ignore parse error
      }

      if (errData?.error) {
        throw new Error(
          `[${errData.error.code}] ${errData.error.message}`
        )
      }
      throw new Error(`HTTP ${res.status}: ${res.statusText}`)
    }

    if (res.status === 204) {
      return {} as T
    }

    return (await res.json()) as T
  }

  async checkHealth(): Promise<{ status: string }> {
    return this.request<{ status: string }>('/healthz')
  }

  async checkReady(): Promise<{ status: string }> {
    return this.request<{ status: string }>('/readyz')
  }

  async listProjects(): Promise<Project[]> {
    const res = await this.request<PageResponse<Project>>('/v1/projects')
    return res.items || []
  }

  async listBaselines(project: string = 'demo'): Promise<Baseline[]> {
    const res = await this.request<PageResponse<Baseline>>(
      `/v1/projects/${encodeURIComponent(project)}/baselines`
    )
    return res.items || []
  }

  async listComponents(project: string = 'demo'): Promise<Component[]> {
    const res = await this.request<PageResponse<Component>>(
      `/v1/projects/${encodeURIComponent(project)}/components`
    )
    return res.items || []
  }

  async listCompositions(project?: string): Promise<Composition[]> {
    const q = project ? `?project=${encodeURIComponent(project)}` : ''
    const res = await this.request<PageResponse<Composition>>(`/v1/compositions${q}`)
    return res.items || []
  }

  async getComposition(id: string): Promise<Composition> {
    return this.request<Composition>(`/v1/compositions/${encodeURIComponent(id)}`)
  }

  async createComposition(
    req: CreateCompositionRequest,
    idempotencyKey?: string
  ): Promise<Composition> {
    const headers: Record<string, string> = {}
    if (idempotencyKey) {
      headers['Idempotency-Key'] = idempotencyKey
    }
    return this.request<Composition>('/v1/compositions', {
      method: 'POST',
      headers,
      body: JSON.stringify(req),
    })
  }

  async updateComposition(
    id: string,
    req: UpdateCompositionRequest
  ): Promise<Composition> {
    return this.request<Composition>(`/v1/compositions/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(req),
    })
  }

  async getComponentLogs(id: string, component: string, signal?: AbortSignal): Promise<ComponentLogs> {
    return this.request<ComponentLogs>(
      `/v1/compositions/${encodeURIComponent(id)}/components/${encodeURIComponent(component)}/logs?tail_lines=200&max_bytes=65536`,
      { signal }
    )
  }

  async listCompositionEvents(id: string, after = '', signal?: AbortSignal): Promise<PageResponse<LifecycleEvent>> {
    const query = new URLSearchParams({ limit: '20', after })
    return this.request<PageResponse<LifecycleEvent>>(
      `/v1/compositions/${encodeURIComponent(id)}/events?${query}`, { signal }
    )
  }

  async destroyComposition(id: string): Promise<Composition> {
    return this.request<Composition>(`/v1/compositions/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    })
  }
}

export const apiClient = new EnvyApiClient()
