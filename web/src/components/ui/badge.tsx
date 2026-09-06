import * as React from "react";
import { cn } from "../../lib/utils";
import { Phase } from "../../types/api";

export interface BadgeProps extends React.HTMLAttributes<HTMLDivElement> {
  variant?:
    | "default"
    | "secondary"
    | "destructive"
    | "outline"
    | "success"
    | "warning";
  phase?: Phase;
}

export function Badge({
  className,
  variant = "default",
  phase,
  children,
  ...props
}: BadgeProps) {
  let badgeVariant = variant;
  let extraClasses = "";

  if (phase) {
    switch (phase) {
      case "ready":
        badgeVariant = "success";
        break;
      case "provisioning":
      case "updating":
        badgeVariant = "warning";
        extraClasses = "animate-pulse";
        break;
      case "failed":
        badgeVariant = "destructive";
        break;
      case "destroying":
      case "destroyed":
        badgeVariant = "secondary";
        extraClasses = "opacity-75";
        break;
      default:
        badgeVariant = "default";
    }
  }

  const variants = {
    default: "border-transparent bg-primary text-primary-foreground",
    secondary: "border-transparent bg-secondary text-secondary-foreground",
    destructive:
      "border-transparent bg-red-950 text-red-200 border border-red-800",
    outline: "text-foreground border border-border",
    success: "border-emerald-800 bg-emerald-950/80 text-emerald-300 border",
    warning: "border-amber-800 bg-amber-950/80 text-amber-300 border",
  };

  return (
    <div
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
        variants[badgeVariant],
        extraClasses,
        className,
      )}
      {...props}
    >
      {phase && (
        <span
          className={cn("h-1.5 w-1.5 rounded-full", {
            "bg-emerald-400": phase === "ready",
            "bg-amber-400": phase === "provisioning" || phase === "updating",
            "bg-red-400": phase === "failed",
            "bg-zinc-400": phase === "destroying" || phase === "destroyed",
            "bg-blue-400": phase === "created",
          })}
        />
      )}
      {children || phase}
    </div>
  );
}
