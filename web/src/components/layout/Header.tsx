import { RefreshCw, Plus, KeyRound, Sun, Moon } from "lucide-react";
import { Button } from "../ui/button";
import { StatusPill } from "./StatusPill";
import { useEnvyApi } from "../../context/ApiContext";
import { useTheme } from "../../context/ThemeContext";
import { NavItem } from "../sidebar/Sidebar";

interface HeaderProps {
  currentTab: NavItem;
  onOpenCreate: () => void;
  onOpenSettings: () => void;
}

export function Header({
  currentTab,
  onOpenCreate,
  onOpenSettings,
}: HeaderProps) {
  const { refreshAll, loading, session, serverStatus } = useEnvyApi();
  const { isDark, setTheme, theme } = useTheme();

  const tabTitles: Record<NavItem, { title: string; subtitle: string }> = {
    compositions: {
      title: "Compositions",
      subtitle:
        "Manage isolated workload overrides combining shared staging baselines",
    },
    create: {
      title: "Create Preview Composition",
      subtitle:
        "Deploy temporary overrides with baggage routing and preview hostnames",
    },
    catalog: {
      title: "Catalog & Components",
      subtitle:
        "Approved component profiles, registered baselines, and override boundaries",
    },
    topology: {
      title: "Routing Intent",
      subtitle:
        "Registered baseline destinations and composition override selection",
    },
    recipes: {
      title: "Environment Recipes",
      subtitle: "Export, validate, and recreate portable environment intent",
    },
    activity: {
      title: "Operational Activity",
      subtitle: "Who changed what, when, and through which interface",
    },
    settings: {
      title: "Installation & Preferences",
      subtitle: "Effective installation identity and local display preferences",
    },
  };

  const { title, subtitle } = tabTitles[currentTab];

  const toggleTheme = () => {
    setTheme(isDark ? "light" : "dark");
  };

  return (
    <header className="h-16 border-b border-border bg-card/80 px-6 flex items-center justify-between backdrop-blur-md sticky top-0 z-10 transition-colors">
      <div>
        <h1 className="text-lg font-semibold tracking-tight text-foreground flex items-center gap-3">
          {title}
        </h1>
        <p className="text-xs text-muted-foreground hidden sm:block">
          {subtitle}
        </p>
      </div>

      <div className="flex items-center gap-2.5">
        <StatusPill className="hidden md:inline-flex" />

        {!session && serverStatus === "disconnected" && (
          <Button
            variant="outline"
            size="sm"
            onClick={onOpenSettings}
            className="text-amber-800 dark:text-amber-300 border-amber-300/80 dark:border-amber-800/80 bg-amber-50 dark:bg-amber-950/40 hover:bg-amber-100 dark:hover:bg-amber-900/50 text-xs gap-1.5 font-medium shadow-2xs"
          >
            <KeyRound className="h-3.5 w-3.5 text-amber-600 dark:text-amber-400" />
            Connection settings
          </Button>
        )}

        <Button
          variant="outline"
          size="sm"
          onClick={toggleTheme}
          className="h-8 w-8 p-0 text-muted-foreground hover:text-foreground"
          title={`Switch to ${isDark ? "light" : "dark"} mode (currently ${theme})`}
        >
          {isDark ? (
            <Sun className="h-4 w-4 text-amber-400" />
          ) : (
            <Moon className="h-4 w-4" />
          )}
          <span className="sr-only">Toggle theme</span>
        </Button>

        <Button
          variant="outline"
          size="sm"
          onClick={() => refreshAll()}
          disabled={loading}
          className="text-xs gap-1.5 text-muted-foreground hover:text-foreground"
        >
          <RefreshCw
            className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`}
          />
          <span className="hidden sm:inline">Refresh</span>
        </Button>

        {currentTab !== "create" && (
          <Button
            size="sm"
            onClick={onOpenCreate}
            className="text-xs gap-1.5 shadow-sm"
          >
            <Plus className="h-3.5 w-3.5" />
            <span>New Preview</span>
          </Button>
        )}
      </div>
    </header>
  );
}
