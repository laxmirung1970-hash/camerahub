import { useEffect } from "react";

const CONTROLS =
  'button:not(:disabled), input:not(:disabled), select:not(:disabled), a[href], summary, [tabindex="0"]';
// Arrow keys move focus between visible controls by screen position, as a TV
// remote does. Inside a text field, left/right edit text and up/down leave it.
export function useRemoteNavigation() {
  useEffect(() => {
    const navigate = (event: KeyboardEvent) => {
      if (!event.key.startsWith("Arrow") || event.altKey || event.ctrlKey || event.metaKey) return;
      const current = document.activeElement as HTMLElement | null;
      const horizontal = event.key === "ArrowLeft" || event.key === "ArrowRight";
      const inText = current?.matches("input:not([type=checkbox]):not([type=radio]), textarea");
      if (inText && horizontal) return;
      if (current?.matches("select") && !horizontal) return;
      const dialogs = document.querySelectorAll<HTMLElement>('[role="dialog"]');
      const scope =
        dialogs[dialogs.length - 1] ||
        document.fullscreenElement ||
        document.querySelector(".monitor.is-expanded") ||
        document;
      const controls = Array.from(scope.querySelectorAll<HTMLElement>(CONTROLS)).filter((el) => {
        const r = el.getBoundingClientRect();
        return r.width > 0 && r.height > 0 && !el.closest("[inert]");
      });
      if (!controls.length) return;
      if (!current || current === document.body || !controls.includes(current)) {
        controls[0].focus();
        event.preventDefault();
        return;
      }
      const r = current.getBoundingClientRect(),
        x = r.left + r.width / 2,
        y = r.top + r.height / 2;
      const positive = event.key === "ArrowRight" || event.key === "ArrowDown";
      const best = controls
        .filter((el) => el !== current)
        .map((el) => {
          const b = el.getBoundingClientRect(),
            dx = b.left + b.width / 2 - x,
            dy = b.top + b.height / 2 - y;
          return {
            el,
            forward: (horizontal ? dx : dy) * (positive ? 1 : -1),
            score: Math.hypot(dx, dy) + Math.abs(horizontal ? dy : dx) * 2.5,
          };
        })
        .filter((c) => c.forward > 3)
        .sort((a, b) => a.score - b.score)[0];
      if (best) {
        event.preventDefault();
        best.el.focus();
        best.el.scrollIntoView({ block: "nearest", inline: "nearest" });
      }
    };
    document.addEventListener("keydown", navigate);
    return () => document.removeEventListener("keydown", navigate);
  }, []);
}
