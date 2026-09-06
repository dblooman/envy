import { RefreshCw, Plus, KeyRound } from 'lucide-react'
import { Button } from '../ui/button'
import { StatusPill } from './StatusPill'
import { useEnvyApi } from '../../context/ApiContext'
import { NavItem } from '../sidebar/Sidebar'

interface HeaderProps {
  currentTab: NavItem
  onOpenCreate: () => void
  onOpenSettings: () => void
}

export function Header({ currentTab, onOpenCreate, onOpenSettings }: HeaderProps) {
  const { refreshAll, loading, token } = useEnvyApi()

  const tabTitles: Record<NavItem, { title: string; subtitle: string }> = {
    compositions: {
      title: 'Compositions',
      subtitle: 'Manage isolated workload overrides combining shared staging baselines',
    },
    create: {
      title: 'Create Preview Composition',
      subtitle: 'Deploy temporary overrides with baggage routing and preview hostnames',
    },
    catalog: {
      title: 'Catalog & Components',
      subtitle: 'Approved component profiles, registered baselines, and override boundaries',
    },
    topology: {
      title: 'Routing & Istio Mesh',
      subtitle: 'Visual representation of dynamic request propagation and baggage headers',
    },
    settings: {
      title: 'Settings & Authentication',
      subtitle: 'Configure local server connection, Bearer API token, and demo mode',
    },
  }

  const { title, subtitle } = tabTitles[currentTab]

  return (
    <header className="h-16 border-b border-border bg-card/40 px-6 flex items-center justify-between backdrop-blur-md sticky top-0 z-10">
      <div>
        <h1 className="text-lg font-semibold tracking-tight text-foreground flex items-center gap-3">
          {title}
        </h1>
        <p className="text-xs text-muted-foreground hidden sm:block">{subtitle}</p>
      </div>

      <div className="flex items-center gap-3">
        <StatusPill className="hidden md:inline-flex" />

        {!token && (
          <Button
            variant="outline"
            size="sm"
            onClick={onOpenSettings}
            className="text-amber-400 border-amber-500/30 bg-amber-500/10 hover:bg-amber-500/20 text-xs gap-1.5"
          >
            <KeyRound className="h-3.5 w-3.5" />
            Set API Token
          </Button>
        )}

        <Button
          variant="outline"
          size="sm"
          onClick={() => refreshAll()}
          disabled={loading}
          className="text-xs gap-1.5"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
          <span className="hidden sm:inline">Refresh</span>
        </Button>

        {currentTab !== 'create' && (
          <Button
            size="sm"
            onClick={onOpenCreate}
            className="text-xs gap-1.5 bg-blue-600 hover:bg-blue-700 text-white"
          >
            <Plus className="h-3.5 w-3.5" />
            <span>New Preview</span>
          </Button>
        )}
      </div>
    </header>
  )
}
