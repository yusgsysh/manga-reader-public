import {
  forwardRef,
  useRef,
  type ButtonHTMLAttributes,
  type ReactNode,
} from "react";
import { cn } from "@cloudflare/kumo";

interface IconButtonProps
  extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children"> {
  label: string;
  children: ReactNode;
  size?: "sm" | "md";
}

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(
  function IconButton(
    { label, children, className, size = "md", type = "button", ...rest },
    ref,
  ) {
    const buttonRef = useRef<HTMLButtonElement | null>(null);

    return (
      <button
        ref={(node) => {
          buttonRef.current = node;
          if (typeof ref === "function") {
            ref(node);
          } else if (ref) {
            ref.current = node;
          }
        }}
        type={type}
        aria-label={label}
        title={label}
        className={cn(
          "icon-button flex shrink-0 items-center justify-center rounded-lg text-kumo-subtle transition-colors hover:bg-kumo-tint hover:text-kumo-default focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--app-accent)] disabled:pointer-events-none disabled:opacity-50",
          size === "sm" ? "size-8" : "size-9",
          className,
        )}
        {...rest}
        onMouseDown={(event) => {
          const button = buttonRef.current;
          if (button) {
            button.classList.remove("icon-button-pop");
            void button.offsetWidth;
            button.classList.add("icon-button-pop");
          }
          rest.onMouseDown?.(event);
        }}
        onAnimationEnd={(event) => {
          event.currentTarget.classList.remove("icon-button-pop");
          rest.onAnimationEnd?.(event);
        }}
      >
        {children}
      </button>
    );
  },
);
