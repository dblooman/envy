import { useEffect, useState } from "react";
import { ThemeProvider } from "./context/ThemeContext";
import { ApiProvider } from "./context/ApiContext";
import { Sidebar, NavItem } from "./components/sidebar/Sidebar";
import { Header } from "./components/layout/Header";
import { CompositionList } from "./components/compositions/CompositionList";
import { CatalogView } from "./components/catalog/CatalogView";
import { TopologyView } from "./components/topology/TopologyView";
import { SettingsView } from "./components/settings/SettingsView";
import { CreateCompositionDialog } from "./components/compositions/CreateCompositionDialog";
import { ActivityView } from "./components/activity/ActivityView";
import { RecipesView } from "./components/recipes/RecipesView";

function routeState() {
  const parts = window.location.pathname.split("/").filter(Boolean);
  const tab = (parts[0] || "compositions") as NavItem;
  const valid: NavItem[] = [
    "compositions",
    "catalog",
    "topology",
    "recipes",
    "activity",
    "settings",
  ];
  return {
    tab: valid.includes(tab) ? tab : "compositions",
    compositionId: parts[0] === "compositions" ? parts[1] || null : null,
  };
}

function AppContent() {
  const initial = routeState();
  const [currentTab, setCurrentTab] = useState<NavItem>(initial.tab);
  const [compositionId, setCompositionId] = useState<string | null>(
    initial.compositionId,
  );
  const [isCollapsed, setIsCollapsed] = useState<boolean>(false);
  const [createDialogOpen, setCreateDialogOpen] = useState<boolean>(false);

  const handleOpenCreate = () => {
    setCreateDialogOpen(true);
  };

  const handleOpenSettings = () => {
    navigate("settings");
  };

  const navigate = (tab: NavItem, id?: string | null) => {
    const path =
      tab === "compositions" && id
        ? `/compositions/${encodeURIComponent(id)}`
        : `/${tab}`;
    window.history.pushState({}, "", path);
    setCurrentTab(tab);
    setCompositionId(id || null);
    window.scrollTo(0, 0);
  };
  useEffect(() => {
    const onPop = () => {
      const next = routeState();
      setCurrentTab(next.tab);
      setCompositionId(next.compositionId);
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-background text-foreground selection:bg-zinc-900 selection:text-white dark:selection:bg-zinc-100 dark:selection:text-zinc-900">
      {/* Sidebar Navigation */}
      <Sidebar
        currentTab={currentTab}
        onTabChange={(tab) => {
          if (tab === "create") {
            setCreateDialogOpen(true);
          } else {
            navigate(tab);
          }
        }}
        isCollapsed={isCollapsed}
        onToggleCollapse={() => setIsCollapsed(!isCollapsed)}
      />

      {/* Main Content Area */}
      <div className="flex-1 flex flex-col min-w-0 h-full overflow-hidden">
        <Header
          currentTab={currentTab}
          onOpenCreate={handleOpenCreate}
          onOpenSettings={handleOpenSettings}
        />

        <main className="flex-1 overflow-y-auto p-4 sm:p-6 lg:p-8">
          <div className="max-w-7xl mx-auto">
            {currentTab === "compositions" && (
              <CompositionList
                onOpenCreate={handleOpenCreate}
                selectedId={compositionId}
                onSelectedIdChange={(id) => navigate("compositions", id)}
              />
            )}

            {currentTab === "catalog" && <CatalogView />}

            {currentTab === "topology" && <TopologyView />}

            {currentTab === "recipes" && <RecipesView />}

            {currentTab === "activity" && <ActivityView />}

            {currentTab === "settings" && <SettingsView />}
          </div>
        </main>
      </div>

      {/* Global Create Dialog */}
      <CreateCompositionDialog
        open={createDialogOpen}
        onOpenChange={setCreateDialogOpen}
        onSuccess={(id) => {
          navigate("compositions", id);
        }}
      />
    </div>
  );
}

export default function App() {
  return (
    <ThemeProvider>
      <ApiProvider>
        <AppContent />
      </ApiProvider>
    </ThemeProvider>
  );
}
