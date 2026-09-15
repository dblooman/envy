import {
  Layers3,
  Plus,
  Boxes,
  Network,
  History,
  FileJson,
  Settings2,
  ChevronLeft,
  ChevronRight,
  X,
} from "lucide-react";
import { useEnvyApi } from "../../context/ApiContext";
import { cn } from "../../lib/utils";
import { StatusPill } from "../layout/StatusPill";

export type NavItem =
  | "compositions"
  | "create"
  | "catalog"
  | "topology"
  | "recipes"
  | "activity"
  | "settings";
interface SidebarProps {
  currentTab: NavItem;
  onTabChange: (tab: NavItem) => void;
  isCollapsed: boolean;
  onToggleCollapse: () => void;
  mobileOpen: boolean;
  onCloseMobile: () => void;
}
const groups = [
  {
    title: "Workspace",
    items: [
      { id: "compositions", label: "Previews", icon: Layers3 },
      { id: "create", label: "Create preview", icon: Plus },
      { id: "recipes", label: "Recipes", icon: FileJson },
      { id: "activity", label: "Activity", icon: History },
    ],
  },
  {
    title: "Platform",
    items: [
      { id: "catalog", label: "Catalog & baselines", icon: Boxes },
      { id: "topology", label: "Routing intent", icon: Network },
      { id: "settings", label: "Installation", icon: Settings2 },
    ],
  },
] as const;
export function Sidebar({
  currentTab,
  onTabChange,
  isCollapsed,
  onToggleCollapse,
  mobileOpen,
  onCloseMobile,
}: SidebarProps) {
  const { compositions, session, installation, isDemoMode } = useEnvyApi();
  const activeCount = compositions.filter(
    (c) => !["destroyed", "failed"].includes(c.phase),
  ).length;
  return (
    <aside
      id="app-navigation"
      className={cn(
        "envy-sidebar",
        isCollapsed && "is-collapsed",
        mobileOpen && "mobile-open",
      )}
    >
      <div className="envy-brand-row">
        <button
          className="envy-brand"
          onClick={() => onTabChange("compositions")}
          aria-label="Envy previews"
        >
          <span className="envy-brand-mark">
            <Layers3 size={23} />
          </span>
          <span className="envy-sidebar-label">
            envy<small>PREVIEW WHAT’S NEXT</small>
          </span>
        </button>
        <button
          className="envy-mobile-close"
          onClick={onCloseMobile}
          aria-label="Close navigation"
        >
          <X size={20} />
        </button>
      </div>
      <div className="envy-installation-label envy-sidebar-label">
        <span className="envy-eyebrow">INSTALLATION</span>
        <strong>
          {installation?.id ||
            (isDemoMode ? "Demo workspace" : "Not connected")}
        </strong>
        {installation?.version && <small>Version {installation.version}</small>}
      </div>
      <nav aria-label="Main navigation">
        {groups.map((group) => (
          <div key={group.title}>
            <p className="envy-nav-label">
              <span className="envy-sidebar-label">{group.title}</span>
            </p>
            {group.items.map(({ id, label, icon: Icon }) => (
              <button
                key={id}
                onClick={() => onTabChange(id)}
                aria-current={currentTab === id ? "page" : undefined}
                aria-label={label}
                title={isCollapsed ? label : undefined}
              >
                <Icon size={18} />
                <span className="envy-sidebar-label">{label}</span>
                {id === "compositions" && (
                  <span className="envy-nav-count envy-sidebar-label">
                    {activeCount}
                  </span>
                )}
              </button>
            ))}
          </div>
        ))}
      </nav>
      <div className="envy-sidebar-footer">
        <div className="envy-sidebar-label">
          <StatusPill />
          <p className="envy-identity">
            {session?.principal.display_name ||
              session?.principal.id ||
              "No identity"}
          </p>
        </div>
        <button
          className="envy-collapse"
          onClick={onToggleCollapse}
          aria-label={isCollapsed ? "Expand sidebar" : "Collapse sidebar"}
        >
          {isCollapsed ? (
            <ChevronRight size={17} />
          ) : (
            <>
              <ChevronLeft size={17} />
              <span>Collapse sidebar</span>
            </>
          )}
        </button>
      </div>
    </aside>
  );
}
