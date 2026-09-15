export type AuthMode =
  "dev" | "password" | "google" | "token" | "proxy" | "none";
export interface AuthConfig {
  mode: AuthMode;
  csrf_token: string;
}
let csrfToken = "";
export async function authConfig(): Promise<AuthConfig> {
  const response = await fetch("/auth/config", { credentials: "same-origin" });
  if (!response.ok) throw new Error("Unable to load login settings.");
  const mismatch =
    "The API returned an unexpected response. Update the Envy server and check that /auth requests reach the API.";
  if (!response.headers.get("content-type")?.includes("application/json"))
    throw new Error(mismatch);
  const config: AuthConfig | null = await response.json().catch(() => null);
  if (
    !config ||
    !["dev", "password", "google", "token", "proxy", "none"].includes(
      config.mode,
    ) ||
    typeof config.csrf_token !== "string"
  )
    throw new Error(mismatch);
  csrfToken = config.csrf_token;
  return config;
}
export async function csrf(): Promise<string> {
  if (!csrfToken) await authConfig();
  return csrfToken;
}
export async function authPost(path: string, body?: unknown) {
  const response = await fetch(path, {
    method: "POST",
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      "X-CSRF-Token": await csrf(),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok)
    throw new Error(
      response.status === 401
        ? "Invalid username or password."
        : "Request failed. Please try again.",
    );
}
export function returnTo(): string {
  const value =
    new URLSearchParams(window.location.search).get("return_to") || "/";
  return value.startsWith("/") &&
    !value.startsWith("//") &&
    !/[\\\r\n]/.test(value)
    ? value
    : "/";
}
export async function signOut(everywhere = false) {
  await authPost(everywhere ? "/auth/logout-all" : "/auth/logout");
  window.location.assign("/login");
}
