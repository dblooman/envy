import React from "react";
import {
  Layers,
  PlusCircle,
  Boxes,
  Network,
  Settings,
  ChevronLeft,
  ChevronRight,
  Server,
  Zap,
} from "lucide-react";
import { useEnvyApi } from "../../context/ApiContext";
import { cn } from "../../lib/utils";
import { StatusPill } from "../layout/StatusPill";
import { Switch } from "../ui/switch";

export type NavItem =
  | "compositions"
  | "create"
  | "catalog"
  | "topology"
  | "settings";

interface SidebarProps {
  currentTab: NavItem;
  onTabChange: (tab: NavItem) => void;
  isCollapsed: boolean;
  onToggleCollapse: () => void;
}

export function Sidebar({
  currentTab,
  onTabChange,
  isCollapsed,
  onToggleCollapse,
}: SidebarProps) {
  const { compositions, isDemoMode, setDemoMode } = useEnvyApi();
  const activeCount = compositions.filter(
    (c) => c.phase !== "destroyed" && c.phase !== "failed",
  ).length;

  const navItems: {
    id: NavItem;
    label: string;
    icon: React.ComponentType<{ className?: string }>;
    badge?: number | string;
  }[] = [
    {
      id: "compositions",
      label: "Compositions",
      icon: Layers,
      badge: activeCount > 0 ? activeCount : undefined,
    },
    {
      id: "create",
      label: "Create Preview",
      icon: PlusCircle,
    },
    {
      id: "catalog",
      label: "Catalog & Profiles",
      icon: Boxes,
    },
    {
      id: "topology",
      label: "Routing & Mesh",
      icon: Network,
    },
    {
      id: "settings",
      label: "Settings & Auth",
      icon: Settings,
    },
  ];

  return (
    <aside
      className={cn(
        "relative flex flex-col border-r border-border bg-sidebar text-sidebar-foreground transition-all duration-300 ease-in-out z-20 select-none shrink-0",
        isCollapsed ? "w-16" : "w-64",
      )}
    >
      {/* Brand Header */}
      <div className="flex items-center justify-between h-16 px-4 border-b border-border">
        {!isCollapsed ? (
          <div className="flex items-center gap-3 overflow-hidden">
            <div className="h-8 w-8 rounded-lg bg-zinc-950 text-white dark:bg-zinc-100 dark:text-zinc-950 flex items-center justify-center shadow-xs shrink-0">
              <Zap className="h-4 w-4 fill-current" />
            </div>
            <div className="flex flex-col min-w-0">
              <div className="flex items-center gap-2">
                <span className="font-bold tracking-tight text-sm text-foreground truncate">
                  ENVY
                </span>
                <span className="text-[10px] font-mono font-medium px-1.5 py-0.5 rounded bg-muted text-muted-foreground border border-border">
                  v0.2.0
                </span>
              </div>
              <span className="text-[11px] text-muted-foreground truncate">
                Ephemeral Previews
              </span>
            </div>
          </div>
        ) : (
          <div className="mx-auto h-8 w-8 rounded-lg bg-zinc-950 text-white dark:bg-zinc-100 dark:text-zinc-950 flex items-center justify-center shadow-xs">
            <Zap className="h-4 w-4 fill-current" />
          </div>
        )}

        <button
          onClick={onToggleCollapse}
          className={cn(
            "p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent transition-colors cursor-pointer",
            isCollapsed && "hidden",
          )}
          title={isCollapsed ? "Expand sidebar" : "Collapse sidebar"}
        >
          <ChevronLeft className="h-4 w-4" />
        </button>
      </div>

      {/* Nav Menu */}
      <div className="flex-1 py-4 px-2 space-y-1 overflow-y-auto">
        {navItems.map((item) => {
          const Icon = item.icon;
          const isActive = currentTab === item.id;

          return (
            <button
              key={item.id}
              onClick={() => onTabChange(item.id)}
              className={cn(
                "w-full flex items-center gap-3 px-3 py-2 rounded-lg text-xs sm:text-sm font-medium transition-all cursor-pointer group relative",
                isActive
                  ? "bg-zinc-900 text-white shadow-xs dark:bg-zinc-100 dark:text-zinc-900"
                  : "text-muted-foreground hover:text-foreground hover:bg-accent/70",
              )}
              title={isCollapsed ? item.label : undefined}
            >
              <Icon
                className={cn(
                  "h-4 w-4 shrink-0 transition-transform group-hover:scale-105",
                  isActive
                    ? "text-inherit"
                    : "text-muted-foreground group-hover:text-foreground",
                )}
              />
              {!isCollapsed && (
                <span className="flex-1 text-left truncate">{item.label}</span>
              )}
              {!isCollapsed && item.badge !== undefined && (
                <span
                  className={cn(
                    "text-[10px] font-mono font-semibold px-2 py-0.5 rounded-full",
                    isActive
                      ? "bg-white/20 text-white dark:bg-zinc-900/20 dark:text-zinc-900"
                      : "bg-muted text-muted-foreground",
                  )}
                >
                  {item.badge}
                </span>
              )}
              {isCollapsed && item.badge !== undefined && (
                <span className="absolute top-1.5 right-1.5 h-2 w-2 rounded-full bg-emerald-500 ring-2 ring-sidebar" />
              )}
            </button>
          );
        })}
      </div>

      {/* Bottom section with simulation toggle and connection info */}
      <div className="p-3 border-t border-border space-y-2">
        {!isCollapsed ? (
          <>
            <div className="p-2.5 rounded-lg bg-card border border-border shadow-2xs flex flex-col gap-2">
              <div className="flex items-center justify-between text-xs">
                <span className="text-muted-foreground flex items-center gap-1.5 font-medium">
                  <Server className="h-3.5 w-3.5" />
                  Status
                </span>
                <StatusPill />
              </div>

              <div className="flex items-center justify-between pt-1 border-t border-border/60 text-[11px]">
                <span className="text-muted-foreground">Demo Simulation</span>
                <Switch
                  checked={isDemoMode}
                  onCheckedChange={(checked) => setDemoMode(checked)}
                  aria-label="Toggle demo simulation mode"
                />
              </div>
            </div>

            <div className="text-[11px] text-muted-foreground px-1 flex items-center justify-between font-mono">
              <span>Port: 8081</span>
              <span className="flex items-center gap-1 text-emerald-600 dark:text-emerald-400 font-sans font-medium text-[11px]">
                <span className="h-1.5 w-1.5 rounded-full bg-emerald-500 animate-pulse" />
                Istio Active
              </span>
            </div>
          </>
        ) : (
          <div className="flex flex-col items-center gap-3">
            <button
              onClick={onToggleCollapse}
              className="p-2 rounded-md hover:bg-accent text-muted-foreground hover:text-foreground cursor-pointer"
              title="Expand sidebar"
            >
              <ChevronRight className="h-4 w-4" />
            </button>
            <div
              className="h-2 w-2 rounded-full bg-emerald-500"
              title="Connected"
            />
          </div>
        )}
      </div>
    </aside>
  );
}
