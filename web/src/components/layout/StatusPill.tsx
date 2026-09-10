import { useEnvyApi } from "../../context/ApiContext";
import { cn } from "../../lib/utils";

export function StatusPill({ className }: { className?: string }) {
  const { serverStatus, isDemoMode } = useEnvyApi();

  let dotColor = "bg-zinc-400";
  let textColor = "text-zinc-600 dark:text-zinc-400";
  let label = "Connecting...";

  if (isDemoMode || serverStatus === "demo") {
    dotColor = "bg-sky-500";
    textColor = "text-sky-700 dark:text-sky-300";
    label = "Demo Simulation";
  } else if (serverStatus === "connected") {
    dotColor = "bg-emerald-500";
    textColor = "text-emerald-700 dark:text-emerald-300";
    label = "Live Connected";
  } else if (serverStatus === "disconnected") {
    dotColor = "bg-rose-500";
    textColor = "text-rose-700 dark:text-rose-300";
    label = "Server Offline";
  }

  return (
    <div
      className={cn(
        "inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-xs font-medium bg-muted/60 border border-border shadow-2xs",
        className,
      )}
    >
      <span className="relative flex h-2 w-2">
        {(serverStatus === "connected" || serverStatus === "demo") && (
          <span
            className={cn(
              "animate-ping absolute inline-flex h-full w-full rounded-full opacity-60",
              dotColor,
            )}
          />
        )}
        <span
          className={cn("relative inline-flex rounded-full h-2 w-2", dotColor)}
        />
      </span>
      <span className={textColor}>{label}</span>
    </div>
  );
}
