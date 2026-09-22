import { RefreshCw, Plus, Sun, Moon, Menu, ChevronRight } from "lucide-react";
import { Button } from "../ui/button";
import { useEnvyApi } from "../../context/ApiContext";
import { useTheme } from "../../context/ThemeContext";
import { NavItem } from "../sidebar/Sidebar";

export const pageTitles: Record<
  NavItem,
  { title: string; description: string; label: string }
> = {
  compositions: {
    title: "Previews",
    description: "Real environments. Only the services you change.",
    label: "Previews",
  },
  create: {
    title: "Create preview",
    description: "Start with shared staging. Deploy only what’s different.",
    label: "Create preview",
  },
  catalog: {
    title: "Applications",
    description:
      "Approved components, registered baselines, and source repositories.",
    label: "Applications",
  },
  topology: {
    title: "Baseline routing",
    description:
      "Explore registered destinations and selected service overrides.",
    label: "Routing intent",
  },
  recipes: {
    title: "Recipes",
    description: "Export, validate, and recreate portable environment recipes.",
    label: "Recipes",
  },
  activity: {
    title: "Activity",
    description: "Who changed what, when, and through which interface.",
    label: "Activity",
  },
  settings: {
    title: "Installation",
    description:
      "Installation identity, display preferences, and account access.",
    label: "Installation",
  },
};
export function Header({
  currentTab,
  onOpenCreate,
  onOpenSettings,
  onToggleMobile,
  mobileOpen,
}: {
  currentTab: NavItem;
  onOpenCreate: () => void;
  onOpenSettings: () => void;
  onToggleMobile: () => void;
  mobileOpen: boolean;
}) {
  const { refreshAll, loading, session, isDemoMode } = useEnvyApi();
  const { isDark, setTheme } = useTheme();
  return (
    <header className="envy-topbar">
      <div className="envy-breadcrumb">
        <button
          className="envy-mobile-toggle"
          onClick={onToggleMobile}
          aria-label="Toggle navigation"
          aria-controls="app-navigation"
          aria-expanded={mobileOpen}
        >
          <Menu size={20} />
        </button>
        <span>Envy</span>
        <ChevronRight size={13} />
        <strong>{pageTitles[currentTab].label}</strong>
      </div>
      <div className="envy-topbar-actions">
        {isDemoMode ? (
          <span className="envy-demo-label">Demo simulation</span>
        ) : session ? (
          <Button variant="ghost" size="sm" onClick={onOpenSettings}>
            {session.principal.display_name || session.principal.id}
          </Button>
        ) : null}
        <Button
          variant="ghost"
          size="icon"
          onClick={() => setTheme(isDark ? "light" : "dark")}
          aria-label={`Switch to ${isDark ? "light" : "dark"} theme`}
        >
          {isDark ? <Sun /> : <Moon />}
        </Button>
        <Button
          variant="ghost"
          size="icon"
          onClick={() => void refreshAll()}
          disabled={loading}
          aria-label="Refresh data"
        >
          <RefreshCw className={loading ? "animate-spin" : ""} />
        </Button>
        {currentTab !== "create" && currentTab !== "compositions" && (
          <Button size="sm" onClick={onOpenCreate}>
            <Plus />
            <span className="hidden sm:inline">New preview</span>
            <span className="sr-only sm:hidden">New preview</span>
          </Button>
        )}
      </div>
    </header>
  );
}
