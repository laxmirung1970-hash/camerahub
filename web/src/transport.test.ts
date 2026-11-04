// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { startStream } from "./transport";

class FakeWS {
  static instances: FakeWS[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((e: { data: unknown }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: (() => void) | null = null;
  binaryType = "";
  closed = false;
  constructor(
    public url: string,
    public protocols?: string[],
  ) {
    FakeWS.instances.push(this);
  }
  send() {}
  close() {
    this.closed = true;
  }
}
class FakeMediaSource extends EventTarget {
  static instances: FakeMediaSource[] = [];
  readyState = "open";
  static isTypeSupported = () => true;
  constructor() {
    super();
    FakeMediaSource.instances.push(this);
  }
  addSourceBuffer() {
    return Object.assign(new EventTarget(), { updating: false, buffered: { length: 0 }, appendBuffer() {} });
  }
}
function video() {
  return Object.assign(new EventTarget(), {
    pause() {},
    load() {},
    play: () => Promise.resolve(),
    removeAttribute() {},
    src: "",
    srcObject: null,
    currentTime: 0,
    playsInline: false,
    canPlayType: () => "",
  }) as unknown as HTMLVideoElement;
}
type Handler = (a: unknown, b: unknown) => void;
class HlsCtor {
  static instances: HlsCtor[] = [];
  static isSupported = () => true;
  static Events = { MANIFEST_PARSED: "p", ERROR: "e" };
  headers: Record<string, string> = {};
  handlers: Record<string, Handler> = {};
  constructor(cfg: { xhrSetup: (x: { withCredentials: boolean; setRequestHeader: (k: string, v: string) => void }) => void }) {
    cfg.xhrSetup({ withCredentials: false, setRequestHeader: (k, v) => (this.headers[k] = v) });
    HlsCtor.instances.push(this);
  }
  loadSource() {}
  attachMedia() {}
  on(name: string, fn: Handler) {
    this.handlers[name] = fn;
  }
  destroy() {}
}
beforeEach(() => {
  vi.useFakeTimers();
  FakeWS.instances = [];
  FakeMediaSource.instances = [];
  HlsCtor.instances = [];
  vi.stubGlobal("WebSocket", FakeWS);
  vi.stubGlobal("MediaSource", FakeMediaSource);
  vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: () => "blob:x", revokeObjectURL: () => {} }));
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});
const opts = (el: HTMLVideoElement, onState = vi.fn()) => ({
  el,
  wsUrl: "ws://h/api/cameras/a/ws",
  hlsUrl: "/api/cameras/a/hls",
  rtcUrl: "/api/cameras/a/webrtc",
  loadHls: async () => HlsCtor as unknown as typeof import("hls.js").default,
  onState,
});
const openSource = () => FakeMediaSource.instances.at(-1)!.dispatchEvent(new Event("sourceopen"));

describe("startStream", () => {
  it("falls from MSE to HLS when the socket closes", async () => {
    const onState = vi.fn();
    const stop = startStream({ ...opts(video(), onState), order: ["mse", "hls"] });
    expect(onState).toHaveBeenLastCalledWith("connecting", { transport: "mse" });
    openSource();
    const ws = FakeWS.instances[0];
    expect(ws.protocols).toBeUndefined();
    ws.onclose?.();
    await vi.advanceTimersByTimeAsync(0);
    expect(ws.closed).toBe(true);
    expect(onState).toHaveBeenLastCalledWith("connecting", { transport: "hls" });
    expect(HlsCtor.instances).toHaveLength(1);
    stop();
  });
  it("advances only once even if close, error and the deadline all fire", async () => {
    const onState = vi.fn();
    startStream({ ...opts(video(), onState), order: ["mse", "hls"] });
    openSource();
    const ws = FakeWS.instances[0];
    ws.onerror?.();
    ws.onclose?.();
    await vi.advanceTimersByTimeAsync(13000);
    expect(onState.mock.calls.filter(([, d]) => (d as { transport?: string }).transport === "hls")).toHaveLength(1);
  });
  it("gives up with the last problem after the final transport fails", async () => {
    const onState = vi.fn();
    startStream({ ...opts(video(), onState), order: ["hls"] });
    await vi.advanceTimersByTimeAsync(0);
    HlsCtor.instances[0].handlers.e({}, { fatal: true, details: "networkError", response: { code: 503 } });
    expect(onState).toHaveBeenLastCalledWith("error", { problem: "networkError 503" });
  });
  it("ignores late events after stop()", async () => {
    const onState = vi.fn();
    const stop = startStream({ ...opts(video(), onState), order: ["mse", "hls"] });
    openSource();
    stop();
    const calls = onState.mock.calls.length;
    FakeWS.instances[0].onclose?.();
    await vi.advanceTimersByTimeAsync(30000);
    expect(onState.mock.calls.length).toBe(calls);
    expect(HlsCtor.instances).toHaveLength(0);
  });
  it("passes the bearer token to hls.js and as the WebSocket subprotocol", async () => {
    startStream({ ...opts(video()), order: ["hls"], token: "tok" });
    await vi.advanceTimersByTimeAsync(0);
    expect(HlsCtor.instances[0].headers.Authorization).toBe("Bearer tok");
    startStream({ ...opts(video()), order: ["mse"], token: "tok" });
    openSource();
    expect(FakeWS.instances[0].protocols).toEqual(["bearer", "tok"]);
  });
  it("stops at an account problem instead of trying the next transport", async () => {
    const onState = vi.fn();
    startStream({ ...opts(video(), onState), order: ["mse", "hls"] });
    openSource();
    FakeWS.instances[0].onmessage?.({ data: JSON.stringify({ type: "error", value: "401 unauthorized" }) });
    await vi.advanceTimersByTimeAsync(0);
    expect(onState).toHaveBeenLastCalledWith("error", { problem: "401 unauthorized" });
    expect(HlsCtor.instances).toHaveLength(0);
  });
});
