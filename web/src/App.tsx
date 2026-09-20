import type { OnboardingTarget } from "./components/catalog/ApplicationOnboarding";
import { useEffect, useRef, useState } from "react";
import { Plus } from "lucide-react";
import { ThemeProvider } from "./context/ThemeContext";
import { ApiProvider, useEnvyApi } from "./context/ApiContext";
import { Sidebar, NavItem } from "./components/sidebar/Sidebar";
import { Header, pageTitles } from "./components/layout/Header";
import { Button } from "./components/ui/button";
import { CompositionList } from "./components/compositions/CompositionList";
import { CatalogView } from "./components/catalog/CatalogView";
import { TopologyView } from "./components/topology/TopologyView";
import { SettingsView } from "./components/settings/SettingsView";
import { CreateCompositionView } from "./components/compositions/CreateCompositionView";
import { ActivityView } from "./components/activity/ActivityView";
import { RecipesView } from "./components/recipes/RecipesView";
import "./workspace.css";
import { AuthGate } from "./components/layout/AuthGate";

export function routeState() {
  const parts = window.location.pathname.split("/").filter(Boolean);
  const candidate = (parts[0] || "compositions") as NavItem;
  const tab = Object.hasOwn(pageTitles, candidate) ? candidate : "compositions";
  let compositionId: string | null = null;
  if (tab === "compositions" && parts[1]) {
    try {
      compositionId = decodeURIComponent(parts[1]);
    } catch {
      compositionId = parts[1];
    }
  }
  return { tab, compositionId, search: window.location.search };
}

export function AppContent() {
  const { isDemoMode, serverStatus, session, installation } = useEnvyApi();
  return (
    <ScopedAppContent
      key={JSON.stringify([
        isDemoMode,
        serverStatus,
        installation?.id,
        session,
      ])}
    />
  );
}
function ScopedAppContent() {
  const [route, setRoute] = useState(routeState);
  const [onboardingTarget, setOnboardingTarget] = useState<OnboardingTarget>();
  const [creationTarget, setCreationTarget] = useState<OnboardingTarget>();
  const [isCollapsed, setIsCollapsed] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const lastListSearch = useRef(
    route.tab === "compositions" ? route.search : "",
  );
  const content = useRef<HTMLElement>(null);
  const title = useRef<HTMLHeadingElement>(null);
  const { error, loading, isDemoMode, serverStatus, session, installation } =
    useEnvyApi();
  const connectionScope = JSON.stringify([
    isDemoMode,
    serverStatus,
    installation?.id,
    session,
  ]);
  const { tab: currentTab, compositionId } = route;
  const navigate = (tab: NavItem, id?: string | null) => {
    if (route.tab === "compositions") lastListSearch.current = route.search;
    const path =
      tab === "compositions" && id
        ? `/compositions/${encodeURIComponent(id)}`
        : `/${tab}`;
    const nextSearch = new URLSearchParams(
      tab === "compositions" ? lastListSearch.current : "",
    );
    if (tab === "compositions" && (!id || id !== route.compositionId))
      nextSearch.delete("section");
    const search = nextSearch.size ? `?${nextSearch}` : "";
    window.history.pushState({}, "", path + search);
    setRoute({ tab, compositionId: id || null, search });
    setMobileOpen(false);
  };
  useEffect(() => {
    const onPop = () => {
      setRoute(routeState());
      setMobileOpen(false);
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);
  useEffect(() => {
    content.current?.scrollTo?.(0, 0);
    title.current?.focus();
  }, [currentTab, compositionId]);
  const page = pageTitles[currentTab];
  return (
    <div className="envy-app">
      <a href="#workspace-content" className="envy-skip">
        Skip to content
      </a>
      <Sidebar
        currentTab={currentTab}
        onTabChange={(tab) => navigate(tab)}
        isCollapsed={isCollapsed}
        onToggleCollapse={() => setIsCollapsed(!isCollapsed)}
        mobileOpen={mobileOpen}
        onCloseMobile={() => {
          setMobileOpen(false);
          content.current?.focus();
        }}
      />
      <div className="envy-workspace">
        <Header
          currentTab={currentTab}
          onOpenCreate={() => navigate("create")}
          onOpenSettings={() => navigate("settings")}
          mobileOpen={mobileOpen}
          onToggleMobile={() => setMobileOpen(!mobileOpen)}
        />
        <main
          id="workspace-content"
          className="envy-main"
          ref={content}
          tabIndex={-1}
        >
          <div className="envy-content">
            {!(currentTab === "compositions" && compositionId) && (
              <div className="envy-page-heading">
                <div>
                  <span className="envy-eyebrow">
                    {["catalog", "topology", "settings"].includes(currentTab)
                      ? "Platform administration"
                      : page.label}
                  </span>
                  <h1 ref={title} tabIndex={-1}>
                    {page.title}
                  </h1>
                  <p>{page.description}</p>
                </div>
                {currentTab === "compositions" && (
                  <Button onClick={() => navigate("create")}>
                    <Plus />
                    New preview
                  </Button>
                )}
              </div>
            )}
            {error && (
              <div role="alert" className="envy-error-banner">
                {error}
              </div>
            )}
            {!error && !isDemoMode && serverStatus === "disconnected" && (
              <div role="status" className="envy-error-banner">
                Envy is disconnected. Open Installation to check your
                connection.
              </div>
            )}
            {loading && (
              <p role="status" className="mb-4 text-sm text-muted-foreground">
                Refreshing workspace…
              </p>
            )}
            {currentTab === "compositions" && (
              <CompositionList
                onOpenCreate={() => navigate("create")}
                selectedId={compositionId}
                onSelectedIdChange={(id) => navigate("compositions", id)}
                query={route.search}
                onQueryChange={(search) => {
                  window.history.replaceState(
                    {},
                    "",
                    window.location.pathname + search,
                  );
                  lastListSearch.current = search;
                  setRoute((prev) => ({ ...prev, search }));
                }}
              />
            )}
            {currentTab === "catalog" && (
              <CatalogView
                key={connectionScope}
                initialTarget={onboardingTarget}
                onCreate={(target) => {
                  setCreationTarget(target);
                  setOnboardingTarget(undefined);
                  navigate("create");
                }}
              />
            )}
            {currentTab === "topology" && <TopologyView />}
            {currentTab === "recipes" && <RecipesView />}
            {currentTab === "activity" && <ActivityView />}
            {currentTab === "settings" && <SettingsView />}
            <CreateCompositionView
              key={`${connectionScope}/${creationTarget?.project}/${creationTarget?.baseline}`}
              initialProject={creationTarget?.project}
              initialBaseline={creationTarget?.baseline}
              onPrepare={(target) => {
                setOnboardingTarget(target);
                navigate("catalog");
              }}
              open={currentTab === "create"}
              onCancel={() => navigate("compositions")}
              onSuccess={(id) => navigate("compositions", id)}
            />
          </div>
        </main>
      </div>
    </div>
  );
}
export default function App() {
  return (
    <ThemeProvider>
      <AuthGate>
        <ApiProvider>
          <AppContent />
        </ApiProvider>
      </AuthGate>
    </ThemeProvider>
  );
}
