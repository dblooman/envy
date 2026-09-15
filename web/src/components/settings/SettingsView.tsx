import { useState } from "react";
import { Laptop, Moon, Server, Sun } from "lucide-react";
import { useEnvyApi } from "../../context/ApiContext";
import { useTheme } from "../../context/ThemeContext";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "../ui/card";
import { Button } from "../ui/button";
import { signOut } from "../../lib/auth";
import { Switch } from "../ui/switch";

export function SettingsView() {
  const { theme, setTheme } = useTheme();
  const { isDemoMode, setDemoMode, session, installation } = useEnvyApi();
  const [section, setSection] = useState("Installation");
  const [error, setError] = useState("");
  return (
    <div className="space-y-6">
      <div className="envy-admin-intro">
        <Server size={22} />
        <div>
          <strong>Configuration with context</strong>
          <p>
            Installation values come from the server. Appearance and demo
            settings apply to this browser.
          </p>
        </div>
      </div>
      <nav className="envy-section-nav" aria-label="Installation sections">
        {["Installation", "Appearance", "Demo"].map((item) => (
          <button
            key={item}
            aria-current={section === item ? "page" : undefined}
            onClick={() => setSection(item)}
          >
            {item}
          </button>
        ))}
      </nav>
      <div className="envy-admin-grid max-w-5xl">
        <Card hidden={section !== "Installation"}>
          <CardHeader>
            <CardTitle className="text-base">Installation</CardTitle>
            <CardDescription>
              Read-only configuration reported by the installation.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 text-sm">
            {installation ? (
              <dl className="grid grid-cols-2 gap-3">
                <div>
                  <dt className="text-muted-foreground">Installation</dt>
                  <dd className="font-mono">{installation.id}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Version</dt>
                  <dd>{installation.version}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Authentication</dt>
                  <dd>{installation.auth_mode}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Preview limit</dt>
                  <dd>{installation.max_compositions}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">
                    Default / maximum TTL
                  </dt>
                  <dd>
                    {installation.default_ttl} / {installation.max_ttl}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Activity retention</dt>
                  <dd>{installation.audit_retention || "retained"}</dd>
                </div>
              </dl>
            ) : (
              <p className="text-muted-foreground">
                Connect to view installation information.
              </p>
            )}
            {session && (
              <div className="rounded-lg border border-border bg-muted/40 p-3">
                <p className="font-medium">
                  {session.principal.display_name || session.principal.id}
                </p>
                <p className="text-muted-foreground">
                  {session.principal.kind} identity · {session.auth_mode}{" "}
                  authentication
                </p>
                {session.principal.email && <p>{session.principal.email}</p>}
                {["password", "google"].includes(session.auth_mode) && (
                  <div className="flex gap-2 pt-3">
                    <Button
                      onClick={() =>
                        void signOut().catch(() =>
                          setError("Unable to sign out. Please retry."),
                        )
                      }
                    >
                      Sign out
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() =>
                        void signOut(true).catch(() =>
                          setError("Unable to sign out. Please retry."),
                        )
                      }
                    >
                      Sign out everywhere
                    </Button>
                  </div>
                )}
                {error && <p role="alert">{error}</p>}
              </div>
            )}
          </CardContent>
        </Card>
        <Card hidden={section !== "Appearance"}>
          <CardHeader>
            <CardTitle className="text-base">Appearance</CardTitle>
            <CardDescription>Stored only on this browser.</CardDescription>
          </CardHeader>
          <CardContent className="grid grid-cols-3 gap-2">
            {(
              [
                { id: "light", label: "Light", icon: Sun },
                { id: "dark", label: "Dark", icon: Moon },
                { id: "system", label: "System", icon: Laptop },
              ] as const
            ).map((option) => {
              const Icon = option.icon;
              return (
                <button
                  key={option.id}
                  onClick={() => setTheme(option.id)}
                  aria-pressed={theme === option.id}
                  className={`rounded-lg border p-3 text-left ${theme === option.id ? "border-primary bg-accent text-accent-foreground" : "border-border"}`}
                >
                  <Icon className="mb-2 h-4 w-4" />
                  <span className="text-sm font-medium">{option.label}</span>
                </button>
              );
            })}
          </CardContent>
        </Card>
        <Card hidden={section !== "Demo"}>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Server className="h-4 w-4" />
              Demo simulation
            </CardTitle>
            <CardDescription>
              Explore sample previews without changing infrastructure.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between rounded-lg border border-border p-3">
              <div>
                <p className="text-sm font-medium">Demo simulation</p>
                <p className="text-xs text-muted-foreground">
                  Explicit local sample data; no API or infrastructure changes.
                </p>
              </div>
              <Switch
                aria-label="Demo simulation"
                checked={isDemoMode}
                onCheckedChange={setDemoMode}
              />
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
