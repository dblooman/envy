import * as React from "react";
import { Popover as BasePopover } from "@base-ui-components/react";
import { cn } from "../../lib/utils";

const Popover = BasePopover.Root;
const PopoverTrigger = BasePopover.Trigger;
const PopoverClose = BasePopover.Close;

const PopoverContent = React.forwardRef<
  HTMLDivElement,
  React.ComponentPropsWithoutRef<typeof BasePopover.Popup> & {
    sideOffset?: number;
    align?: "start" | "center" | "end";
  }
>(
  (
    { className, align = "center", sideOffset = 4, children, ...props },
    ref,
  ) => (
    <BasePopover.Portal>
      <BasePopover.Positioner sideOffset={sideOffset} align={align}>
        <BasePopover.Popup
          ref={ref}
          className={cn(
            "z-50 w-72 rounded-md border border-border bg-popover p-4 text-popover-foreground shadow-md outline-none transition-opacity data-ending-style:opacity-0 data-starting-style:opacity-0",
            className,
          )}
          {...props}
        >
          {children}
        </BasePopover.Popup>
      </BasePopover.Positioner>
    </BasePopover.Portal>
  ),
);
PopoverContent.displayName = "PopoverContent";

export { Popover, PopoverTrigger, PopoverContent, PopoverClose };
