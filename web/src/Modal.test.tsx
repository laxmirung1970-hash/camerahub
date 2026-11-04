// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Modal } from "./ui";
import { Clock } from "./Clock";

afterEach(cleanup);

it("Modal traps Tab across textarea and summary and closes on Escape", () => {
  const onClose = vi.fn();
  render(
    <Modal title="t" onClose={onClose}>
      <textarea aria-label="a" />
      <details>
        <summary>more</summary>
      </details>
    </Modal>,
  );
  const summary = screen.getByText("more");
  summary.focus();
  fireEvent.keyDown(document, { key: "Tab" });
  expect(document.activeElement?.getAttribute("aria-label")).toBe("ปิดหน้าต่าง");
  fireEvent.keyDown(document, { key: "Escape" });
  expect(onClose).toHaveBeenCalledTimes(1);
});

it("Clock renders Thai time and ticks on its own", () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-11-04T03:04:05Z"));
  render(<Clock />);
  expect(screen.getByText("10:04:05")).toBeTruthy();
  act(() => vi.advanceTimersByTime(1000));
  expect(screen.getByText("10:04:06")).toBeTruthy();
  vi.useRealTimers();
});
