import { describe, expect, it } from "vitest";
import { reconnectDelay, requiresAccountFix } from "./reconnectPolicy";

describe("automatic recovery after a long camera outage", () => {
  it("backs off but keeps a bounded retry after the third failure", () => {
    const values = [0, 1, 2, 3, 20].map((n) => reconnectDelay(n, "camera-1"));
    expect(values[1] - values[0]).toBe(10_000);
    expect(values[2] - values[1]).toBe(20_000);
    expect(values[3]).toBeGreaterThanOrEqual(60_000);
    expect(values[4]).toBe(values[3]);
    expect(values[4]).toBeLessThan(63_000);
    expect(reconnectDelay(20, "camera-2")).not.toBe(values[4]);
  });
  it("does not repeatedly try credentials that a camera rejected", () => {
    for (const problem of [
      "auth",
      "401 unauthorized",
      "403",
      "bad credential",
    ]) {
      expect(requiresAccountFix(problem)).toBe(true);
    }
    expect(requiresAccountFix("timeout")).toBe(false);
    expect(requiresAccountFix("gateway unavailable")).toBe(false);
  });
});
