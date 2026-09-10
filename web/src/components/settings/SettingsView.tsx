import React, { useState } from "react";
import {
  KeyRound,
  Server,
  Sparkles,
  CheckCircle2,
  AlertCircle,
  Terminal,
  RefreshCw,
  Sun,
  Moon,
  Laptop,
} from "lucide-react";
import { useEnvyApi } from "../../context/ApiContext";
import { useTheme } from "../../context/ThemeContext";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "../ui/card";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Switch } from "../ui/switch";

export function SettingsView() {
  const {
    serverUrl,
    setServerUrl,
    token,
    setToken,
    isDemoMode,
    setDemoMode,
    testConnection,
  } = useEnvyApi();
  const { theme, setTheme } = useTheme();

  const [inputUrl, setInputUrl] = useState(serverUrl);
  const [inputToken, setInputToken] = useState(token);
  const [testResult, setTestResult] = useState<{
    ok: boolean;
    message: string;
  } | null>(null);
  const [testing, setTesting] = useState(false);
  const [savedSuccess, setSavedSuccess] = useState(false);

  const handleSave = (e: React.FormEvent) => {
    e.preventDefault();
    setServerUrl(inputUrl.trim());
    setToken(inputToken.trim());
    setSavedSuccess(true);
    setTimeout(() => setSavedSuccess(false), 2500);
  };

  const handleTest = async () => {
    setTesting(true);
    setTestResult(null);
    setServerUrl(inputUrl.trim());
    setToken(inputToken.trim());
    try {
      const res = await testConnection();
      setTestResult(res);
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="max-w-3xl space-y-6">
      {/* Theme & Appearance Card */}
      <Card className="border-border bg-card shadow-2xs">
        <CardHeader className="pb-3">
          <CardTitle className="text-base flex items-center gap-2">
            <Sun className="h-4 w-4 text-foreground" />
            Appearance & Theme
          </CardTitle>
          <CardDescription>
            Switch between Crisp Daylight (Light) and Refined Neutral Slate
            (Dark).
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-3 gap-3">
            {[
              {
                id: "light",
                label: "Daylight Light",
                icon: Sun,
                desc: "Clean slate & white canvas",
              },
              {
                id: "dark",
                label: "Refined Dark",
                icon: Moon,
                desc: "Neutral graphite slate",
              },
              {
                id: "system",
                label: "System Auto",
                icon: Laptop,
                desc: "Follow OS preference",
              },
            ].map((opt) => {
              const Icon = opt.icon;
              const isSelected = theme === opt.id;
              return (
                <button
                  key={opt.id}
                  type="button"
                  onClick={() =>
                    setTheme(opt.id as "light" | "dark" | "system")
                  }
                  className={`p-3 rounded-lg border text-left transition-all cursor-pointer flex flex-col justify-between gap-1.5 ${
                    isSelected
                      ? "border-zinc-900 bg-zinc-100 text-zinc-950 ring-1 ring-zinc-900 dark:border-zinc-100 dark:bg-zinc-800 dark:text-zinc-50 dark:ring-zinc-100 shadow-2xs"
                      : "border-border bg-card text-muted-foreground hover:border-zinc-400 dark:hover:border-zinc-600"
                  }`}
                >
                  <div className="flex items-center justify-between w-full">
                    <Icon className="h-4 w-4 text-foreground" />
                    {isSelected && (
                      <span className="h-2 w-2 rounded-full bg-emerald-500" />
                    )}
                  </div>
                  <div>
                    <div className="text-xs font-semibold text-foreground">
                      {opt.label}
                    </div>
                    <div className="text-[10px] text-muted-foreground">
                      {opt.desc}
                    </div>
                  </div>
                </button>
              );
            })}
          </div>
        </CardContent>
      </Card>

      {/* Simulation / Live Switch */}
      <Card className="border-border bg-card shadow-2xs">
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <CardTitle className="text-base flex items-center gap-2">
              <Sparkles className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
              Demo & Simulation Mode
            </CardTitle>
            <Switch
              checked={isDemoMode}
              onCheckedChange={(checked) => setDemoMode(checked)}
              aria-label="Toggle demo mode"
            />
          </div>
          <CardDescription>
            Enables instant preview exploration with simulated lifecycle
            progression without requiring a live Kind/Istio cluster.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-xs text-muted-foreground">
          {isDemoMode ? (
            <div className="p-3 rounded-lg bg-sky-50 border border-sky-200 text-sky-900 dark:bg-sky-950/40 dark:border-sky-900 dark:text-sky-300 flex items-center gap-2">
              <CheckCircle2 className="h-4 w-4 text-sky-600 dark:text-sky-400 shrink-0" />
              <span>
                Demo mode is currently <strong>Active</strong>. You can create,
                update, and destroy preview compositions safely.
              </span>
            </div>
          ) : (
            <div className="p-3 rounded-lg bg-muted/60 border border-border text-foreground flex items-center gap-2">
              <Server className="h-4 w-4 text-emerald-600 dark:text-emerald-400 shrink-0" />
              <span>
                Live Mode is active. API requests are directed to the Envy
                server at{" "}
                <code className="font-mono font-medium">
                  {inputUrl || "http://127.0.0.1:8081"}
                </code>
                .
              </span>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Control Plane Server Settings */}
      <Card className="border-border bg-card shadow-2xs">
        <form onSubmit={handleSave}>
          <CardHeader>
            <CardTitle className="text-base flex items-center gap-2">
              <Server className="h-4 w-4 text-foreground" />
              Control Plane API Settings
            </CardTitle>
            <CardDescription>
              Connect to Envy HTTP server. Default port is{" "}
              <code className="font-mono text-foreground font-semibold">
                :8081
              </code>
              .
            </CardDescription>
          </CardHeader>

          <CardContent className="space-y-4 text-xs sm:text-sm">
            {testResult && (
              <div
                className={`p-3 rounded-lg border text-xs flex items-center gap-2 ${
                  testResult.ok
                    ? "bg-emerald-50 border-emerald-200 text-emerald-800 dark:bg-emerald-950/60 dark:border-emerald-800 dark:text-emerald-200"
                    : "bg-rose-50 border-rose-200 text-rose-800 dark:bg-rose-950/60 dark:border-rose-800 dark:text-rose-200"
                }`}
              >
                {testResult.ok ? (
                  <CheckCircle2 className="h-4 w-4 text-emerald-600 dark:text-emerald-400 shrink-0" />
                ) : (
                  <AlertCircle className="h-4 w-4 text-rose-600 dark:text-rose-400 shrink-0" />
                )}
                <span>{testResult.message}</span>
              </div>
            )}

            {savedSuccess && (
              <div className="p-2.5 rounded-lg bg-emerald-50 border border-emerald-200 text-emerald-800 dark:bg-emerald-950/40 dark:border-emerald-800/60 dark:text-emerald-300 text-xs flex items-center gap-2">
                <CheckCircle2 className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
                <span>Settings saved to local storage!</span>
              </div>
            )}

            {/* Server URL */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">
                Server Base URL
              </label>
              <Input
                value={inputUrl}
                onChange={(e) => setInputUrl(e.target.value)}
                placeholder="Leave blank for automatic Vite proxy (http://127.0.0.1:8081)"
                className="font-mono text-xs"
              />
              <p className="text-[11px] text-muted-foreground">
                Leaving this blank routes through Vite dev server proxy to
                prevent CORS issues.
              </p>
            </div>

            {/* API Bearer Token */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground flex items-center gap-1.5">
                <KeyRound className="h-3.5 w-3.5 text-foreground" />
                API Bearer Token
              </label>
              <Input
                type="password"
                value={inputToken}
                onChange={(e) => setInputToken(e.target.value)}
                placeholder="Paste token from .envy/envy-dev/api-token"
                className="font-mono text-xs"
              />
              <div className="p-2.5 rounded-lg bg-muted/50 border border-border text-[11px] text-muted-foreground space-y-1">
                <div className="flex items-center gap-1.5 font-medium text-foreground">
                  <Terminal className="h-3 w-3" /> Quick Shell Command to Copy
                  Token:
                </div>
                <code className="block bg-background px-2 py-1 rounded border border-border text-[10px] text-foreground font-mono select-all">
                  cat .envy/envy-dev/api-token | pbcopy
                </code>
              </div>
            </div>
          </CardContent>

          <CardFooter className="flex items-center justify-between border-t border-border pt-4">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={handleTest}
              disabled={testing}
              className="text-xs gap-1.5"
            >
              <RefreshCw
                className={`h-3.5 w-3.5 ${testing ? "animate-spin" : ""}`}
              />
              Test Connection
            </Button>

            <Button type="submit" size="sm" className="text-xs shadow-sm">
              Save Configuration
            </Button>
          </CardFooter>
        </form>
      </Card>
    </div>
  );
}
