import { useState } from "react";
import {
  CheckCircle2,
  KeyRound,
  Laptop,
  Moon,
  Server,
  Sun,
} from "lucide-react";
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
import { Input } from "../ui/input";
import { Switch } from "../ui/switch";

export function SettingsView() {
  const { theme, setTheme } = useTheme();
  const {
    serverUrl,
    setServerUrl,
    token,
    setToken,
    isDemoMode,
    setDemoMode,
    testConnection,
    session,
    installation,
  } = useEnvyApi();
  const [section, setSection] = useState("Installation");
  const [url, setURL] = useState(serverUrl);
  const [developmentToken, setDevelopmentToken] = useState(token);
  const [result, setResult] = useState("");
  const [testing, setTesting] = useState(false);
  async function test() {
    setTesting(true);
    setResult("");
    const response = await testConnection(url.trim(), developmentToken.trim());
    if (response.ok) {
      setServerUrl(url.trim());
      setToken(developmentToken.trim());
    }
    setResult(response.message);
    setTesting(false);
  }
  return (
    <div className="space-y-6">
      <div className="envy-admin-intro">
        <Server size={22} />
        <div>
          <strong>Configuration with context</strong>
          <p>
            Installation values come from the server. Appearance and development
            connection settings apply to this browser.
          </p>
        </div>
      </div>
      <nav className="envy-section-nav" aria-label="Installation sections">
        {["Installation", "Appearance", "Connection"].map((item) => (
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
        <Card hidden={section !== "Connection"}>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Server className="h-4 w-4" />
              Development connection
            </CardTitle>
            <CardDescription>
              Deployed Envy uses the same origin and its configured external,
              anonymous, or bearer authentication. These controls are for local
              development.
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
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="text-sm font-medium">
                Server URL
                <Input
                  value={url}
                  onChange={(e) => setURL(e.target.value)}
                  placeholder="Same origin"
                  className="mt-1 font-mono"
                />
              </label>
              <label className="text-sm font-medium">
                <span className="flex items-center gap-1">
                  <KeyRound className="h-4 w-4" />
                  Legacy development token
                </span>
                <Input
                  type="password"
                  value={developmentToken}
                  onChange={(e) => setDevelopmentToken(e.target.value)}
                  placeholder="Held in memory only"
                  className="mt-1 font-mono"
                />
              </label>
            </div>
            <div className="flex items-center gap-3">
              <Button onClick={() => void test()} disabled={testing}>
                {testing ? "Testing…" : "Apply and test"}
              </Button>
              {result && (
                <p role="status" className="flex items-center gap-2 text-sm">
                  <CheckCircle2 className="h-4 w-4" />
                  {result}
                </p>
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              The token is cleared on reload and is never written to browser
              storage. Authentication and provider credentials remain startup
              configuration.
            </p>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
