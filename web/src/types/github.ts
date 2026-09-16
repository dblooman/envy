export interface GitHubStatus {
  configured: boolean;
  webhook_configured: boolean;
  last_webhook_at?: string;
  last_reconciled_at?: string;
  error?: string;
}
export interface GitHubInstallation {
  id: number;
  account: { login: string };
  permissions: Record<string, string>;
}
export interface GitHubRepository {
  id: number;
  full_name: string;
}
export interface PreviewPolicy {
  project: string;
  repository: string;
  github_repository_id?: number;
  enabled: boolean;
  baseline: string;
  components: string[];
  ttl: string;
  workflow_id: number;
}
export interface PRPreview {
  deployment_status?: string;
  id: string;
  policy: PreviewPolicy;
  number: number;
  pr_url: string;
  status: string;
  reason?: string;
  requested_sha: string;
  deployed_sha: string;
  composition_id?: string;
  generation: number;
  url?: string;
  expires_at?: string;
  terminal: boolean;
  feedback_error?: string;
}
