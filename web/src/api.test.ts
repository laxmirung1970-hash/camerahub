// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { API_BASE, api, apiUrl, onUnauthorized, tokenStore, wsUrl } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("api base", () => {
  it("uses same-origin relative paths when VITE_API_BASE is empty", () => {
    expect(API_BASE).toBe("");
    expect(apiUrl("/cameras")).toBe("/api/cameras");
    expect(wsUrl("/cameras/x/ws")).toBe(`ws://${location.host}/api/cameras/x/ws`);
  });
  it("never stores a token for same-origin deployments", () => {
    tokenStore.set("abc");
    expect(tokenStore.get()).toBeNull();
  });
});
describe("api()", () => {
  it("resolves undefined on 204 and on non-JSON bodies", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 204 })));
    await expect(api("/devices/x", undefined, "DELETE")).resolves.toBeUndefined();
  });
  it("reports 401 through onUnauthorized and throws the server message", async () => {
    const seen = vi.fn();
    onUnauthorized(seen);
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify({ error: "หมดเวลา" }), { status: 401, headers: { "content-type": "application/json" } })),
    );
    await expect(api("/cameras")).rejects.toThrow("หมดเวลา");
    expect(seen).toHaveBeenCalledTimes(1);
  });
  it("sends JSON bodies with credentials", async () => {
    const fetchMock = vi.fn(async () => new Response("{}", { status: 200, headers: { "content-type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    await api("/login", { password: "x" });
    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(init.method).toBe("POST");
    expect(init.credentials).toBe("include");
    expect((init.headers as Record<string, string>)["Content-Type"]).toBe("application/json");
  });
});
