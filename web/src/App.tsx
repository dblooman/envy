import { useState } from 'react'
import { ApiProvider } from './context/ApiContext'
import { Sidebar, NavItem } from './components/sidebar/Sidebar'
import { Header } from './components/layout/Header'
import { CompositionList } from './components/compositions/CompositionList'
import { CatalogView } from './components/catalog/CatalogView'
import { TopologyView } from './components/topology/TopologyView'
import { SettingsView } from './components/settings/SettingsView'
import { CreateCompositionDialog } from './components/compositions/CreateCompositionDialog'

function AppContent() {
  const [currentTab, setCurrentTab] = useState<NavItem>('compositions')
  const [isCollapsed, setIsCollapsed] = useState<boolean>(false)
  const [createDialogOpen, setCreateDialogOpen] = useState<boolean>(false)

  const handleOpenCreate = () => {
    setCreateDialogOpen(true)
  }

  const handleOpenSettings = () => {
    setCurrentTab('settings')
  }

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-background text-foreground">
      {/* Sidebar Navigation */}
      <Sidebar
        currentTab={currentTab}
        onTabChange={(tab) => {
          if (tab === 'create') {
            setCreateDialogOpen(true)
          } else {
            setCurrentTab(tab)
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
            {currentTab === 'compositions' && (
              <CompositionList onOpenCreate={handleOpenCreate} />
            )}

            {currentTab === 'catalog' && <CatalogView />}

            {currentTab === 'topology' && <TopologyView />}

            {currentTab === 'settings' && <SettingsView />}
          </div>
        </main>
      </div>

      {/* Global Create Dialog */}
      <CreateCompositionDialog
        open={createDialogOpen}
        onOpenChange={setCreateDialogOpen}
        onSuccess={() => {
          setCurrentTab('compositions')
        }}
      />
    </div>
  )
}

export default function App() {
  return (
    <ApiProvider>
      <AppContent />
    </ApiProvider>
  )
}
