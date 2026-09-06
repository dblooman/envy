import React, { useState } from "react";
import {
  KeyRound,
  Server,
  Sparkles,
  CheckCircle2,
  AlertCircle,
  Terminal,
  RefreshCw,
} from "lucide-react";
import { useEnvyApi } from "../../context/ApiContext";
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
    // temporarily apply to client for testing
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
      {/* Simulation / Live Switch */}
      <Card className="border-border bg-card/60">
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <CardTitle className="text-base flex items-center gap-2">
              <Sparkles className="h-4 w-4 text-sky-400" />
              Demo & Simulation Mode
            </CardTitle>
            <button
              onClick={() => setDemoMode(!isDemoMode)}
              className={`relative inline-flex h-5 w-10 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none ${
                isDemoMode ? "bg-sky-500" : "bg-muted"
              }`}
            >
              <span
                className={`pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
                  isDemoMode ? "translate-x-5" : "translate-x-0"
                }`}
              />
            </button>
          </div>
          <CardDescription>
            Enables instant preview exploration with simulated lifecycle
            progression without requiring a live Kind/Istio cluster.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-xs text-muted-foreground">
          {isDemoMode ? (
            <div className="p-3 rounded-lg bg-sky-950/40 border border-sky-800/60 text-sky-200 flex items-center gap-2">
              <CheckCircle2 className="h-4 w-4 text-sky-400 shrink-0" />
              <span>
                Demo mode is currently{" "}
                <strong className="text-white">Active</strong>. You can create,
                update, and destroy preview compositions safely.
              </span>
            </div>
          ) : (
            <div className="p-3 rounded-lg bg-secondary/60 border border-border text-foreground flex items-center gap-2">
              <Server className="h-4 w-4 text-emerald-400 shrink-0" />
              <span>
                Live Mode is active. API requests are directed to the Envy
                server at{" "}
                <code className="font-mono">
                  {inputUrl || "http://127.0.0.1:8081"}
                </code>
                .
              </span>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Control Plane Server Settings */}
      <Card className="border-border bg-card/60">
        <form onSubmit={handleSave}>
          <CardHeader>
            <CardTitle className="text-base flex items-center gap-2">
              <Server className="h-4 w-4 text-primary" />
              Control Plane API Settings
            </CardTitle>
            <CardDescription>
              Connect to Envy HTTP server. Default port is{" "}
              <code className="font-mono text-primary">:8081</code>.
            </CardDescription>
          </CardHeader>

          <CardContent className="space-y-4 text-xs sm:text-sm">
            {testResult && (
              <div
                className={`p-3 rounded-lg border text-xs flex items-center gap-2 ${
                  testResult.ok
                    ? "bg-emerald-950/60 border-emerald-800 text-emerald-200"
                    : "bg-red-950/60 border-red-800 text-red-200"
                }`}
              >
                {testResult.ok ? (
                  <CheckCircle2 className="h-4 w-4 text-emerald-400 shrink-0" />
                ) : (
                  <AlertCircle className="h-4 w-4 text-red-400 shrink-0" />
                )}
                <span>{testResult.message}</span>
              </div>
            )}

            {savedSuccess && (
              <div className="p-2.5 rounded-lg bg-emerald-950/40 border border-emerald-800/60 text-emerald-300 text-xs flex items-center gap-2">
                <CheckCircle2 className="h-4 w-4" />
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
                <KeyRound className="h-3.5 w-3.5 text-amber-400" />
                API Bearer Token
              </label>
              <Input
                type="password"
                value={inputToken}
                onChange={(e) => setInputToken(e.target.value)}
                placeholder="Paste token from .envy/envy-dev/api-token"
                className="font-mono text-xs"
              />
              <div className="p-2.5 rounded bg-secondary/60 border border-border/80 text-[11px] text-muted-foreground space-y-1">
                <div className="flex items-center gap-1.5 font-medium text-foreground">
                  <Terminal className="h-3 w-3" /> Quick Shell Command to Copy
                  Token:
                </div>
                <code className="block bg-background px-2 py-1 rounded text-[10px] text-primary font-mono select-all">
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

            <Button
              type="submit"
              size="sm"
              className="text-xs bg-blue-600 hover:bg-blue-700 text-white"
            >
              Save Configuration
            </Button>
          </CardFooter>
        </form>
      </Card>
    </div>
  );
}
