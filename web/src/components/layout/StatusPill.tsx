import { useEnvyApi } from '../../context/ApiContext'
import { cn } from '../../lib/utils'

export function StatusPill({ className }: { className?: string }) {
  const { serverStatus, isDemoMode } = useEnvyApi()

  let color = 'bg-zinc-500'
  let textColor = 'text-zinc-400'
  let label = 'Connecting...'

  if (isDemoMode || serverStatus === 'demo') {
    color = 'bg-sky-400'
    textColor = 'text-sky-400'
    label = 'Demo Simulation'
  } else if (serverStatus === 'connected') {
    color = 'bg-emerald-400'
    textColor = 'text-emerald-400'
    label = 'Connected'
  } else if (serverStatus === 'disconnected') {
    color = 'bg-rose-500'
    textColor = 'text-rose-400'
    label = 'Server Offline'
  }

  return (
    <div
      className={cn(
        'inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-xs font-medium bg-secondary/80 border border-border/80',
        className
      )}
    >
      <span className="relative flex h-2 w-2">
        {(serverStatus === 'connected' || serverStatus === 'demo') && (
          <span
            className={cn(
              'animate-ping absolute inline-flex h-full w-full rounded-full opacity-75',
              color
            )}
          />
        )}
        <span className={cn('relative inline-flex rounded-full h-2 w-2', color)} />
      </span>
      <span className={textColor}>{label}</span>
    </div>
  )
}
