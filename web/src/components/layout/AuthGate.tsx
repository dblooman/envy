import { useEffect, useState, type ReactNode, type FormEvent } from "react";
import {
  authConfig,
  authPost,
  returnTo,
  type AuthConfig,
} from "../../lib/auth";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "../ui/card";

export function AuthGate({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<AuthConfig | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "login" | "error">(
    localStorage.getItem("envy_demo_mode") === "true" ? "ready" : "loading",
  );
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  useEffect(() => {
    if (localStorage.getItem("envy_demo_mode") === "true") return;
    let active = true;
    const expired = () => {
      setError("Your session has expired. Sign in again.");
      setState("login");
      void authConfig()
        .then((cfg) => {
          if (active) setConfig(cfg);
        })
        .catch(() => {
          if (active) {
            setError("Unable to load login settings.");
            setState("error");
          }
        });
    };
    window.addEventListener("envy-unauthorized", expired);
    void (async () => {
      try {
        const cfg = await authConfig();
        const session = await fetch("/v1/session", {
          credentials: "same-origin",
        });
        if (!active) return;
        setConfig(cfg);
        if (session.ok) {
          if (window.location.pathname === "/login") {
            window.location.replace(returnTo());
            return;
          }
          setState("ready");
        } else if (session.status === 401) {
          setState("login");
          if (new URLSearchParams(window.location.search).has("error"))
            setError(
              "Sign-in was cancelled or this account is not allowed access.",
            );
        } else {
          throw new Error("Unable to check your session.");
        }
      } catch (e) {
        if (active) {
          setError(e instanceof Error ? e.message : "Login unavailable.");
          setState("error");
        }
      }
    })();
    return () => {
      active = false;
      window.removeEventListener("envy-unauthorized", expired);
    };
  }, []);
  if (state === "ready") return children;
  const destination =
    window.location.pathname === "/login"
      ? returnTo()
      : window.location.pathname + window.location.search;
  async function login(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await authPost("/auth/password", { username, password });
      window.location.assign(destination);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Login failed.");
      setBusy(false);
    }
  }
  return (
    <main className="min-h-screen flex items-center justify-center bg-background p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Sign in to Envy</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {state === "loading" && <p role="status">Checking your session…</p>}
          {error && <p role="alert">{error}</p>}
          {state === "error" && (
            <Button onClick={() => window.location.reload()}>Retry</Button>
          )}
          {state === "login" && config?.mode === "password" && (
            <form onSubmit={(event) => void login(event)} className="space-y-4">
              <label className="block space-y-1">
                Username
                <Input
                  autoComplete="username"
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  required
                />
              </label>
              <label className="block space-y-1">
                Password
                <Input
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  required
                />
              </label>
              <Button type="submit" disabled={busy}>
                {busy ? "Signing in…" : "Sign in"}
              </Button>
            </form>
          )}
          {state === "login" && config?.mode === "google" && (
            <Button
              onClick={() =>
                window.location.assign(
                  `/auth/google/start?return_to=${encodeURIComponent(destination)}`,
                )
              }
            >
              Continue with Google
            </Button>
          )}
          {state === "login" &&
            config &&
            !["password", "google"].includes(config.mode) && (
              <p>
                This installation uses {config.mode} authentication. Access it
                through its configured gateway, or configure password or Google
                login on the server.
              </p>
            )}
          <Button
            variant="ghost"
            onClick={() => {
              localStorage.setItem("envy_demo_mode", "true");
              window.location.assign("/");
            }}
          >
            Explore demo
          </Button>
        </CardContent>
      </Card>
    </main>
  );
}
