// Framework-free stream runner: tries transports in order, each with its own
// deadline and cleanup set. A generation counter makes every late callback
// (ws.onclose after stop, a timer from an abandoned attempt) a no-op.
export type Transport = "webrtc" | "mse" | "hls";
export type StreamPhase = "connecting" | "playing" | "error";
export interface StreamOptions {
  el: HTMLVideoElement;
  wsUrl: string;
  hlsUrl: string;
  rtcUrl: string;
  order: Transport[];
  token?: string | null;
  loadHls?: () => Promise<typeof import("hls.js").default>;
  onState: (
    phase: StreamPhase,
    detail: { transport?: Transport; problem?: string },
  ) => void;
}
const DEADLINE: Record<Transport, number> = {
  webrtc: 5000,
  mse: 12000,
  hls: 20000,
};
const MSE_CODECS =
  "avc1.640029,avc1.64002A,avc1.640033,hvc1.1.6.L153.B0,mp4a.40.2,opus,flac";

export function startStream(o: StreamOptions): () => void {
  const { el } = o;
  let gen = 0;
  const cleanups = new Set<() => void>();
  let lastProblem = "";
  let hasPlayed = false;
  const live = (g: number) => g === gen;
  const flush = () => {
    for (const fn of cleanups) {
      try {
        fn();
      } catch {
        /* ignore */
      }
    }
    cleanups.clear();
    el.pause();
    el.removeAttribute("src");
    el.srcObject = null;
    el.load();
  };
  const play = () => void el.play().catch(() => {});
  const onPlaying = () => {
    hasPlayed = true;
    lastProblem = "";
    o.onState("playing", { transport: current });
  };
  el.addEventListener("playing", onPlaying);
  let current: Transport | undefined;

  const run = (i: number) => {
    const g = ++gen;
    flush();
    if (i >= o.order.length) {
      o.onState("error", { problem: lastProblem || "timeout" });
      return;
    }
    current = o.order[i];
    hasPlayed = false;
    o.onState("connecting", { transport: current });
    let done = false;
    const fail = (reason: string) => {
      if (done || !live(g)) return;
      done = true;
      lastProblem = reason || lastProblem;
      if (/auth|401|403|credential|password/i.test(reason)) {
        flush();
        o.onState("error", { problem: reason });
        return;
      }
      run(i + 1);
    };
    const deadline = setTimeout(() => {
      if (!hasPlayed) fail(lastProblem || "timeout");
    }, DEADLINE[current]);
    cleanups.add(() => clearTimeout(deadline));
    const stopDeadline = () => clearTimeout(deadline);
    el.addEventListener("playing", stopDeadline, { once: true });
    cleanups.add(() => el.removeEventListener("playing", stopDeadline));
    transports[current](g, fail);
  };

  const transports: Record<
    Transport,
    (g: number, fail: (r: string) => void) => void
  > = {
    webrtc(g, fail) {
      if (!window.RTCPeerConnection) return fail("unsupported");
      const pc = new RTCPeerConnection({ iceServers: [] });
      const abort = new AbortController();
      cleanups.add(() => {
        pc.onconnectionstatechange = null;
        pc.ontrack = null;
        pc.close();
        abort.abort();
      });
      pc.addTransceiver("video", { direction: "recvonly" });
      pc.addTransceiver("audio", { direction: "recvonly" });
      pc.ontrack = (e) => {
        if (!live(g)) return;
        el.srcObject = e.streams[0] || new MediaStream([e.track]);
        play();
      };
      pc.onconnectionstatechange = () => {
        if (pc.connectionState === "failed") fail("rtc failed");
      };
      void (async () => {
        try {
          await pc.setLocalDescription(await pc.createOffer());
          await new Promise<void>((resolve) => {
            if (pc.iceGatheringState === "complete") return resolve();
            const t = setTimeout(resolve, 1500);
            const onGather = () => {
              if (pc.iceGatheringState === "complete") {
                clearTimeout(t);
                resolve();
              }
            };
            pc.addEventListener("icegatheringstatechange", onGather);
            cleanups.add(() => {
              clearTimeout(t);
              pc.removeEventListener("icegatheringstatechange", onGather);
            });
          });
          if (!live(g)) return;
          const res = await fetch(o.rtcUrl, {
            method: "POST",
            body: pc.localDescription?.sdp,
            credentials: "include",
            headers: {
              "Content-Type": "application/sdp",
              ...(o.token ? { Authorization: `Bearer ${o.token}` } : {}),
            },
            signal: abort.signal,
          });
          if (!res.ok) throw new Error(`rtc ${res.status}`);
          const sdp = await res.text();
          if (!live(g)) return;
          await pc.setRemoteDescription({ type: "answer", sdp });
        } catch (e) {
          if (live(g)) fail((e as Error).message || "rtc");
        }
      })();
    },
    mse(g, fail) {
      if (!window.MediaSource) return fail("unsupported");
      const ms = new MediaSource();
      const url = URL.createObjectURL(ms);
      let ws: WebSocket | undefined;
      let buffer: SourceBuffer | undefined;
      let queue: ArrayBuffer[] = [];
      cleanups.add(() => {
        if (ws) {
          ws.onopen = ws.onmessage = ws.onerror = ws.onclose = null;
          ws.close();
        }
        URL.revokeObjectURL(url);
        queue = [];
      });
      el.src = url;
      const drain = () => {
        if (!live(g) || !buffer || buffer.updating || ms.readyState !== "open")
          return;
        try {
          if (
            buffer.buffered.length &&
            el.currentTime > 15 &&
            buffer.buffered.start(0) < el.currentTime - 12
          ) {
            buffer.remove(0, el.currentTime - 10);
            return;
          }
          const data = queue.shift();
          if (data) buffer.appendBuffer(data);
        } catch {
          fail("mse append");
        }
      };
      ms.addEventListener(
        "sourceopen",
        () => {
          if (!live(g)) return;
          ws = new WebSocket(o.wsUrl, o.token ? ["bearer", o.token] : undefined);
          ws.binaryType = "arraybuffer";
          ws.onopen = () =>
            ws?.send(JSON.stringify({ type: "mse", value: MSE_CODECS }));
          ws.onmessage = (event) => {
            if (!live(g)) return;
            try {
              if (typeof event.data === "string") {
                const msg = JSON.parse(event.data);
                if (msg.type === "mse") {
                  if (!MediaSource.isTypeSupported(msg.value))
                    return fail("unsupported codec");
                  buffer = ms.addSourceBuffer(msg.value);
                  buffer.addEventListener("updateend", () => {
                    if (!live(g) || !buffer) return;
                    if (buffer.buffered.length) {
                      const end = buffer.buffered.end(buffer.buffered.length - 1);
                      if (end - el.currentTime > 3)
                        el.currentTime = Math.max(0, end - 0.4);
                    }
                    drain();
                    play();
                  });
                } else if (msg.type === "error")
                  fail(msg.value || "camera unavailable");
              } else {
                if (queue.length > 100) return fail("mse backlog");
                queue.push(event.data as ArrayBuffer);
                drain();
              }
            } catch {
              fail("mse message");
            }
          };
          ws.onerror = () => fail("gateway");
          ws.onclose = () => fail("stalled");
          play();
        },
        { once: true },
      );
    },
    hls(g, fail) {
      void (async () => {
        const Hls = await (o.loadHls ?? (() => import("hls.js").then((m) => m.default)))();
        if (!live(g)) return;
        if (Hls.isSupported()) {
          const hls = new Hls({
            liveSyncDurationCount: 2,
            maxBufferLength: 12,
            xhrSetup: (x) => {
              x.withCredentials = true;
              if (o.token) x.setRequestHeader("Authorization", `Bearer ${o.token}`);
            },
          });
          cleanups.add(() => hls.destroy());
          hls.loadSource(o.hlsUrl);
          hls.attachMedia(el);
          hls.on(Hls.Events.MANIFEST_PARSED, play);
          hls.on(Hls.Events.ERROR, (_e, d) => {
            if (d.fatal) fail(`${d.details} ${d.response?.code || ""}`.trim());
          });
        } else if (el.canPlayType("application/vnd.apple.mpegurl")) {
          const onError = () => fail("hls native");
          el.addEventListener("error", onError);
          cleanups.add(() => el.removeEventListener("error", onError));
          el.src = o.hlsUrl;
          play();
        } else fail("unsupported codec");
      })();
    },
  };

  // Stall watchdog is armed only after the first frame; startup has its own deadlines.
  let lastTime = -1,
    lastProgress = Date.now();
  const watchdog = setInterval(() => {
    if (!hasPlayed) return;
    if (el.currentTime !== lastTime) {
      lastTime = el.currentTime;
      lastProgress = Date.now();
    } else if (Date.now() - lastProgress > 15000) {
      gen++;
      flush();
      o.onState("error", { problem: "stalled" });
    }
  }, 3000);

  el.playsInline = true;
  run(0);
  return () => {
    gen++;
    clearInterval(watchdog);
    el.removeEventListener("playing", onPlaying);
    flush();
  };
}
