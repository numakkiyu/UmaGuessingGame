
import * as SelectPrimitive from "@radix-ui/react-select";
import { Check, ChevronDown } from "lucide-react";
import { useId, type ComponentProps, type ReactNode } from "react";
import { cn } from "@/lib/utils";

export const Select = SelectPrimitive.Root;
export const SelectValue = SelectPrimitive.Value;
export const SelectGroup = SelectPrimitive.Group;

export function SelectTrigger({ className, children, ...props }: ComponentProps<typeof SelectPrimitive.Trigger>) {
  return (
    <SelectPrimitive.Trigger
      className={cn(
        "uma-select-trigger flex min-h-11 min-w-0 w-full items-center justify-between gap-2 rounded-[var(--radius-control)]",
        "border border-[var(--color-line-strong)] bg-[var(--color-panel)] px-3 py-2",
        "text-left text-base text-[var(--color-ink)]",
        "focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--color-blue-deep)]",
        "disabled:cursor-not-allowed disabled:opacity-70",
        "data-[placeholder]:text-[var(--color-muted)]",
        className,
      )}
      {...props}
    >
      <span className="min-w-0 flex-1 [overflow-wrap:anywhere]">{children}</span>
      <SelectPrimitive.Icon asChild>
        <ChevronDown aria-hidden="true" className="uma-select-chevron size-4 shrink-0 text-[var(--color-muted)]" />
      </SelectPrimitive.Icon>
    </SelectPrimitive.Trigger>
  );
}

export function SelectContent({ className, children, position = "popper", sideOffset = 8, ...props }: ComponentProps<typeof SelectPrimitive.Content>) {
  return (
    <SelectPrimitive.Portal>
      <SelectPrimitive.Content
        position={position}
        sideOffset={sideOffset}
        className={cn(
          "uma-select-content z-[var(--z-select)] max-h-[min(20rem,var(--radix-select-content-available-height))] min-w-[var(--radix-select-trigger-width)] max-w-[min(28rem,calc(100vw-1.5rem))] overflow-y-auto",
          "rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel)] p-1",
          "shadow-[var(--shadow-panel)]",
          className,
        )}
        {...props}
      >
        <SelectPrimitive.Viewport className="p-0">{children}</SelectPrimitive.Viewport>
      </SelectPrimitive.Content>
    </SelectPrimitive.Portal>
  );
}

export function SelectItem({ className, children, ...props }: ComponentProps<typeof SelectPrimitive.Item>) {
  return (
    <SelectPrimitive.Item
      className={cn(
        "relative flex min-h-11 cursor-default select-none items-center gap-2 rounded-[var(--radius-control)] px-3 py-2 pr-9",
        "uma-select-item text-base text-[var(--color-ink)] outline-none [overflow-wrap:anywhere]",
        "data-[highlighted]:bg-[var(--color-panel-soft)] data-[state=checked]:font-semibold",
        "data-[disabled]:pointer-events-none data-[disabled]:opacity-60",
        className,
      )}
      {...props}
    >
      <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
      <SelectPrimitive.ItemIndicator className="absolute right-2 inline-flex items-center">
        {/* 选中同时有勾选符号，不只靠底色 */}
        <Check aria-hidden="true" className="size-4 text-[var(--color-action-green)]" />
      </SelectPrimitive.ItemIndicator>
    </SelectPrimitive.Item>
  );
}

export type SelectChoice = {
  value: string | number;
  label: ReactNode;
  disabled?: boolean;
  lang?: string;
};

type SelectFieldProps = Omit<ComponentProps<typeof SelectPrimitive.Trigger>, "value" | "onChange" | "children"> & {
  value: string | number;
  onValueChange: (value: string) => void;
  options: readonly (SelectChoice | null | undefined | false | "")[];
  name?: string;
  required?: boolean;
};

/** A single styled control for language, setup and in-game choices, including an empty choice. */
export function SelectField({ value, onValueChange, options, name, required, disabled, ...trigger }: SelectFieldProps) {
  const emptyValue = useId();
  const choices = options.filter((option): option is SelectChoice => !!option);
  return <Select name={name} required={required} disabled={disabled} value={value === "" ? emptyValue : String(value)} onValueChange={next => onValueChange(next === emptyValue ? "" : next)}>
    <SelectTrigger {...trigger} value={String(value)}><SelectValue /></SelectTrigger>
    <SelectContent>{choices.map(option => <SelectItem key={option.value} value={option.value === "" ? emptyValue : String(option.value)} data-value={option.value} disabled={option.disabled} lang={option.lang}>{option.label}</SelectItem>)}</SelectContent>
  </Select>;
}

export function SelectLabel({ className, ...props }: ComponentProps<typeof SelectPrimitive.Label>) {
  return (
    <SelectPrimitive.Label
      className={cn("px-3 py-1.5 text-[length:var(--text-aux)] font-semibold text-[var(--color-muted)]", className)}
      {...props}
    />
  );
}
