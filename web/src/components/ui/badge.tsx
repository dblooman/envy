import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";
import { Phase } from "../../types/api";

const badgeVariants = cva(
  "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-[11px] font-medium tracking-tight transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
  {
    variants: {
      variant: {
        default:
          "border-transparent bg-primary text-primary-foreground hover:bg-primary/80",
        secondary:
          "border-border bg-secondary text-secondary-foreground hover:bg-secondary/80",
        destructive:
          "border-rose-200/80 bg-rose-50 text-rose-800 dark:border-rose-900/60 dark:bg-rose-950/40 dark:text-rose-300",
        outline: "text-foreground border-border bg-background",
        success:
          "border-emerald-200/80 bg-emerald-50 text-emerald-800 dark:border-emerald-900/60 dark:bg-emerald-950/40 dark:text-emerald-300",
        warning:
          "border-amber-200/80 bg-amber-50 text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-300",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

export interface BadgeProps
  extends
    React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {
  phase?: Phase;
}

function Badge({ className, variant, phase, children, ...props }: BadgeProps) {
  let resolvedVariant = variant || "default";

  if (phase) {
    switch (phase) {
      case "ready":
      case "completed":
        resolvedVariant = "success";
        break;
      case "provisioning":
      case "updating":
        resolvedVariant = "warning";
        break;
      case "failed":
        resolvedVariant = "destructive";
        break;
      case "destroying":
      case "destroyed":
      case "cancelled":
        resolvedVariant = "secondary";
        break;
      default:
        resolvedVariant = "secondary";
    }
  }

  return (
    <div
      className={cn(badgeVariants({ variant: resolvedVariant }), className)}
      {...props}
    >
      {phase && (
        <span
          className={cn("h-1.5 w-1.5 rounded-full shrink-0", {
            "bg-emerald-600 dark:bg-emerald-400":
              phase === "ready" || phase === "completed",
            "bg-amber-500 dark:bg-amber-400":
              phase === "provisioning" || phase === "updating",
            "bg-rose-600 dark:bg-rose-400": phase === "failed",
            "bg-zinc-400 dark:bg-zinc-500":
              phase === "destroying" ||
              phase === "destroyed" ||
              phase === "cancelled",
            "bg-zinc-700 dark:bg-zinc-300":
              phase === "created" || phase === "suspended",
          })}
        />
      )}
      {children || phase}
    </div>
  );
}

export { Badge, badgeVariants };
