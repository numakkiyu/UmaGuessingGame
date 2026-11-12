
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  [
    "inline-flex items-center justify-center gap-2 whitespace-nowrap",
    "rounded-[var(--radius-control-lg)] font-semibold",
    "transition-[background-color,border-color,color,transform,opacity] duration-[var(--duration-quick)] ease-[var(--ease-primary)]",
    "select-none",
    // / ：按下 scale 0.98，释放恢复，不挪动布局
    "active:scale-[0.98] motion-reduce:active:scale-100",
    /*
     * 禁用态：不用 opacity 整体变淡。
     * 实测白字 on 60% 透明的 action-green 只有约 2.1:1，禁用按钮上的文字读不清。
     * 改为显式配色：浅底 + 深字（ink on panel-soft = 11.7:1），语义由 disabled 属性
     * 与 cursor 表达，不靠降低对比度表达「不可点」。
     */
    "disabled:pointer-events-none",
    "disabled:bg-[var(--color-panel-soft)] disabled:text-[var(--color-muted)]",
    "disabled:border disabled:border-[var(--color-line-strong)] disabled:shadow-none",
    "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]",
  ].join(" "),
  {
    variants: {
      variant: {
        primary:
          "bg-[var(--color-action-green)] text-white hover:bg-[var(--color-action-green-hover)]",
        brand: "bg-[var(--color-brand-green)] text-[var(--color-ink)] hover:brightness-95",
        secondary:
          "border border-[var(--color-line-strong)] bg-[var(--color-panel)] text-[var(--color-ink)] hover:bg-[var(--color-panel-soft)]",
        ghost: "text-[var(--color-ink)] hover:bg-[var(--color-panel-soft)]",
        link: "text-[var(--color-blue-deep)] underline-offset-4 hover:underline",
        danger: "bg-[var(--color-danger)] text-white hover:brightness-95",
      },
      size: {
        // 主操作 48px
        primary: "min-h-12 px-5 py-3 text-base",
        // 次操作最低 44px
        default: "min-h-11 px-4 py-2.5 text-base",
        sm: "min-h-11 px-3 py-2 text-sm",
        // 图标按钮：视觉 20–24，触摸区仍 ≥44
        icon: "size-11 p-0",
        "icon-sm": "size-11 p-0 [&_svg]:size-5",
      },
      block: {
        true: "w-full",
      },
    },
    defaultVariants: {
      variant: "secondary",
      size: "default",
    },
  },
);

export type ButtonProps = ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean;
  };

export function Button({ className, variant, size, block, asChild = false, ...props }: ButtonProps) {
  const Comp = asChild ? Slot : "button";
  return <Comp className={cn(buttonVariants({ variant, size, block }), className)} {...props} />;
}

export { buttonVariants };
