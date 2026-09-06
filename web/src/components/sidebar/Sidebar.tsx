import React from 'react'
import {
  Layers,
  PlusCircle,
  Boxes,
  Network,
  Settings,
  ChevronLeft,
  ChevronRight,
  Sparkles,
  Server,
  Zap,
} from 'lucide-react'
import { useEnvyApi } from '../../context/ApiContext'
import { cn } from '../../lib/utils'
import { StatusPill } from '../layout/StatusPill'

export type NavItem = 'compositions' | 'create' | 'catalog' | 'topology' | 'settings'

interface SidebarProps {
  currentTab: NavItem
  onTabChange: (tab: NavItem) => void
  isCollapsed: boolean
  onToggleCollapse: () => void
}

export function Sidebar({
  currentTab,
  onTabChange,
  isCollapsed,
  onToggleCollapse,
}: SidebarProps) {
  const { compositions, isDemoMode, setDemoMode } = useEnvyApi()
  const activeCount = compositions.filter(
    (c) => c.phase !== 'destroyed' && c.phase !== 'failed'
  ).length

  const navItems: {
    id: NavItem
    label: string
    icon: React.ComponentType<{ className?: string }>
    badge?: number | string
  }[] = [
    {
      id: 'compositions',
      label: 'Compositions',
      icon: Layers,
      badge: activeCount > 0 ? activeCount : undefined,
    },
    {
      id: 'create',
      label: 'Create Preview',
      icon: PlusCircle,
    },
    {
      id: 'catalog',
      label: 'Catalog & Profiles',
      icon: Boxes,
    },
    {
      id: 'topology',
      label: 'Routing & Mesh',
      icon: Network,
    },
    {
      id: 'settings',
      label: 'Settings & Auth',
      icon: Settings,
    },
  ]

  return (
    <aside
      className={cn(
        'relative flex flex-col border-r border-border bg-sidebar text-sidebar-foreground transition-all duration-300 ease-in-out z-20 select-none shrink-0',
        isCollapsed ? 'w-16' : 'w-64'
      )}
    >
      {/* Brand Header */}
      <div className="flex items-center justify-between h-16 px-4 border-b border-border">
        {!isCollapsed ? (
          <div className="flex items-center gap-3 overflow-hidden">
            <div className="h-9 w-9 rounded-lg bg-gradient-to-tr from-blue-600 via-indigo-500 to-sky-400 flex items-center justify-center text-white shadow-lg shadow-blue-500/20 shrink-0">
              <Zap className="h-5 w-5 fill-white" />
            </div>
            <div className="flex flex-col min-w-0">
              <div className="flex items-center gap-2">
                <span className="font-bold tracking-tight text-base text-foreground truncate">
                  ENVY
                </span>
                <span className="text-[10px] font-mono font-medium px-1.5 py-0.5 rounded bg-blue-500/10 text-blue-400 border border-blue-500/20">
                  v0.2.0
                </span>
              </div>
              <span className="text-[11px] text-muted-foreground truncate">
                Ephemeral Previews
              </span>
            </div>
          </div>
        ) : (
          <div className="mx-auto h-9 w-9 rounded-lg bg-gradient-to-tr from-blue-600 via-indigo-500 to-sky-400 flex items-center justify-center text-white shadow-md">
            <Zap className="h-5 w-5 fill-white" />
          </div>
        )}

        <button
          onClick={onToggleCollapse}
          className={cn(
            'p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent transition-colors cursor-pointer',
            isCollapsed && 'hidden'
          )}
          title={isCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          <ChevronLeft className="h-4 w-4" />
        </button>
      </div>

      {/* Nav Menu */}
      <div className="flex-1 py-4 px-2 space-y-1 overflow-y-auto">
        {navItems.map((item) => {
          const Icon = item.icon
          const isActive = currentTab === item.id

          return (
            <button
              key={item.id}
              onClick={() => onTabChange(item.id)}
              className={cn(
                'w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-all cursor-pointer group relative',
                isActive
                  ? 'bg-primary text-primary-foreground shadow-sm shadow-primary/20'
                  : 'text-muted-foreground hover:text-foreground hover:bg-accent/60'
              )}
              title={isCollapsed ? item.label : undefined}
            >
              <Icon
                className={cn(
                  'h-4 w-4 shrink-0 transition-transform group-hover:scale-110',
                  isActive ? 'text-primary-foreground' : 'text-muted-foreground group-hover:text-foreground'
                )}
              />
              {!isCollapsed && (
                <span className="flex-1 text-left truncate">{item.label}</span>
              )}
              {!isCollapsed && item.badge !== undefined && (
                <span
                  className={cn(
                    'text-[10px] font-semibold px-2 py-0.5 rounded-full',
                    isActive
                      ? 'bg-white/20 text-white'
                      : 'bg-primary/20 text-primary-foreground'
                  )}
                >
                  {item.badge}
                </span>
              )}
              {isCollapsed && item.badge !== undefined && (
                <span className="absolute top-1.5 right-1.5 h-2 w-2 rounded-full bg-blue-500 ring-2 ring-sidebar" />
              )}
            </button>
          )
        })}
      </div>

      {/* Bottom section with simulation toggle and connection info */}
      <div className="p-3 border-t border-border space-y-2">
        {!isCollapsed ? (
          <>
            <div className="p-2.5 rounded-lg bg-card/60 border border-border/80 flex flex-col gap-2">
              <div className="flex items-center justify-between text-xs">
                <span className="text-muted-foreground flex items-center gap-1.5 font-medium">
                  <Server className="h-3.5 w-3.5" />
                  Status
                </span>
                <StatusPill />
              </div>

              <div className="flex items-center justify-between pt-1 border-t border-border/40 text-[11px]">
                <span className="text-muted-foreground">Demo Mode</span>
                <button
                  onClick={() => setDemoMode(!isDemoMode)}
                  className={cn(
                    'relative inline-flex h-4 w-8 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                    isDemoMode ? 'bg-sky-500' : 'bg-muted'
                  )}
                >
                  <span
                    className={cn(
                      'pointer-events-none inline-block h-3 w-3 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                      isDemoMode ? 'translate-x-4' : 'translate-x-0'
                    )}
                  />
                </button>
              </div>
            </div>

            <div className="text-[11px] text-muted-foreground/80 px-1 flex items-center justify-between">
              <span>Port: 8081</span>
              <span className="flex items-center gap-1 text-emerald-400">
                <Sparkles className="h-3 w-3" /> Istio Active
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
            <div className="h-2 w-2 rounded-full bg-emerald-400" title="Connected" />
          </div>
        )}
      </div>
    </aside>
  )
}
