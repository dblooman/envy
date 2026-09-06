import { useState } from "react";
import {
  Network,
  ArrowRight,
  Zap,
  Globe,
  Radio,
  Sparkles,
  Server,
} from "lucide-react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "../ui/card";

export function TopologyView() {
  const [selectedScenario, setSelectedScenario] = useState<
    "baseline" | "composition"
  >("composition");

  return (
    <div className="space-y-6">
      {/* Intro & Scenario Selector */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 p-4 rounded-xl border border-border bg-card/60">
        <div className="space-y-1">
          <h3 className="font-semibold text-foreground text-sm flex items-center gap-2">
            <Network className="h-4 w-4 text-primary" />
            Selective Request Routing & Baggage Propagation
          </h3>
          <p className="text-xs text-muted-foreground">
            Envy provisions only the overridden microservice while sharing
            baseline dependencies, databases, and caches.
          </p>
        </div>

        <div className="flex items-center gap-1.5 bg-muted/60 p-1 rounded-lg">
          <button
            onClick={() => setSelectedScenario("baseline")}
            className={`px-3 py-1.5 rounded-md text-xs font-medium transition-all cursor-pointer ${
              selectedScenario === "baseline"
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            Baseline Flow
          </button>
          <button
            onClick={() => setSelectedScenario("composition")}
            className={`px-3 py-1.5 rounded-md text-xs font-medium transition-all cursor-pointer ${
              selectedScenario === "composition"
                ? "bg-primary text-primary-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            Preview Composition Flow
          </button>
        </div>
      </div>

      {/* Visual Interactive Diagram */}
      <Card className="border-border bg-card/40 overflow-hidden">
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="text-base flex items-center gap-2">
              <Sparkles className="h-4 w-4 text-amber-400" />
              Istio Ingress & Envoy Mesh Topology
            </CardTitle>
            <span className="text-xs font-mono text-muted-foreground">
              {selectedScenario === "composition"
                ? "Host: cmp-01999999-0000.envy.localhost"
                : "Host: baseline.envy.localhost"}
            </span>
          </div>
          <CardDescription>
            {selectedScenario === "composition"
              ? "Incoming requests match the preview hostname. Istio injects the baggage header `baggage: envy-composition=0199...`. Gateway and Service A propagate baggage without being redeployed."
              : "Direct requests to baseline host follow default Istio routes without baggage overrides."}
          </CardDescription>
        </CardHeader>

        <CardContent className="py-6">
          <div className="flex flex-col lg:flex-row items-center justify-center gap-4 lg:gap-6 relative">
            {/* Step 1: Ingress / Client */}
            <div className="flex flex-col items-center p-4 rounded-xl border border-blue-500/30 bg-blue-950/20 text-center w-48 shadow-sm">
              <Globe className="h-6 w-6 text-blue-400 mb-2" />
              <div className="font-semibold text-xs text-foreground">
                Client / Browser
              </div>
              <div className="text-[10px] text-muted-foreground mt-1 font-mono">
                {selectedScenario === "composition"
                  ? "cmp-<id>.envy.localhost"
                  : "baseline.envy.localhost"}
              </div>
            </div>

            <ArrowRight className="h-5 w-5 text-muted-foreground shrink-0 hidden lg:block" />

            {/* Step 2: Istio Ingress Gateway */}
            <div className="flex flex-col items-center p-4 rounded-xl border border-purple-500/30 bg-purple-950/20 text-center w-52 shadow-sm">
              <Zap className="h-6 w-6 text-purple-400 mb-2" />
              <div className="font-semibold text-xs text-foreground">
                Istio Ingress Gateway
              </div>
              <div className="text-[10px] text-purple-300/80 mt-1">
                {selectedScenario === "composition"
                  ? "Injects Baggage Context"
                  : "Default Routing"}
              </div>
            </div>

            <ArrowRight className="h-5 w-5 text-muted-foreground shrink-0 hidden lg:block" />

            {/* Step 3: Shared Gateway & Service A */}
            <div className="flex flex-col gap-2 w-56">
              <div className="p-3 rounded-lg border border-border bg-card text-center">
                <div className="text-[10px] uppercase tracking-wider font-semibold text-zinc-400">
                  Shared Baseline
                </div>
                <div className="font-semibold text-xs text-foreground mt-0.5">
                  Gateway (v1)
                </div>
                <div className="text-[10px] text-muted-foreground">
                  Propagates baggage
                </div>
              </div>
              <div className="p-3 rounded-lg border border-border bg-card text-center">
                <div className="text-[10px] uppercase tracking-wider font-semibold text-zinc-400">
                  Shared Baseline
                </div>
                <div className="font-semibold text-xs text-foreground mt-0.5">
                  Service A (v1)
                </div>
                <div className="text-[10px] text-muted-foreground">
                  Propagates baggage
                </div>
              </div>
            </div>

            <ArrowRight className="h-5 w-5 text-muted-foreground shrink-0 hidden lg:block" />

            {/* Step 4: Routing Destination */}
            <div className="flex flex-col gap-3 w-56">
              {/* Override Pod */}
              <div
                className={`p-3.5 rounded-lg border transition-all text-center ${
                  selectedScenario === "composition"
                    ? "border-emerald-500/60 bg-emerald-950/40 ring-1 ring-emerald-500/40 shadow-lg"
                    : "border-border/40 bg-card/20 opacity-40"
                }`}
              >
                <div className="flex items-center justify-center gap-1.5 text-emerald-400 text-xs font-semibold">
                  <Radio className="h-3.5 w-3.5 animate-pulse" />
                  Override: service-b (v2 / v3)
                </div>
                <div className="text-[10px] text-emerald-300/80 mt-1">
                  Selected by Baggage Header match
                </div>
              </div>

              {/* Baseline Pod */}
              <div
                className={`p-3.5 rounded-lg border transition-all text-center ${
                  selectedScenario === "baseline"
                    ? "border-blue-500/60 bg-blue-950/40 ring-1 ring-blue-500/40 shadow-lg"
                    : "border-border/40 bg-card/20 opacity-40"
                }`}
              >
                <div className="flex items-center justify-center gap-1.5 text-foreground text-xs font-semibold">
                  <Server className="h-3.5 w-3.5" />
                  Baseline: service-b (v1)
                </div>
                <div className="text-[10px] text-muted-foreground mt-1">
                  Default Fallback Destination
                </div>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Key Principles Grid */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4 text-xs">
        <div className="p-4 rounded-xl border border-border bg-card/50 space-y-2">
          <div className="font-semibold text-foreground flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-blue-400" />
            Zero Redundant Duplication
          </div>
          <p className="text-muted-foreground">
            Only the modified service pod is created. Shared databases, Redis
            caches, and upstream services are reused without copying.
          </p>
        </div>

        <div className="p-4 rounded-xl border border-border bg-card/50 space-y-2">
          <div className="font-semibold text-foreground flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-emerald-400" />
            W3C Baggage Standard
          </div>
          <p className="text-muted-foreground">
            Propagates context through OpenTelemetry baggage headers across
            internal microservice calls without modifying intermediate code.
          </p>
        </div>

        <div className="p-4 rounded-xl border border-border bg-card/50 space-y-2">
          <div className="font-semibold text-foreground flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-purple-400" />
            Decoupled Control Plane
          </div>
          <p className="text-muted-foreground">
            The Envy HTTP server acts as the control plane; it configures Istio
            and exits the critical request data path entirely.
          </p>
        </div>
      </div>
    </div>
  );
}
