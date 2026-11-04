import { readStorage, writeStorage } from "./storage";

export interface Camera {
  playbackMode?: "native" | "h264";
  id: string;
  vendor: "tapo" | "xiaomi";
  ref: string;
  name: string;
  group: string;
  model: string;
  lens?: number;
  lensCount?: number;
  host: string;
  favorite: boolean;
  status: string;
  message?: string;
  audio: boolean;
  ptz: boolean;
  verification: string;
  audioVerified: boolean;
  ptzVerified: boolean;
}
export interface Bootstrap {
  mode: "demo" | "live";
  needsSetup: boolean;
  authenticated: boolean;
  name: string;
  kind: string;
  version: string;
  webRTC: boolean;
}
export interface Connection {
  id: string;
  vendor: string;
  name: string;
  account?: string;
  region?: string;
  enabled: boolean;
  status: string;
  message?: string;
}
export interface System {
  mode: string;
  bridge: boolean;
  appMemoryMB: number;
  architecture: string;
  events: { id: number; time: string; kind: string; message: string }[];
  remoteURL: string;
}

// Empty base = same origin (Windows live, NAS). A Vercel build sets VITE_API_BASE
// to the home backend's HTTPS origin and authenticates with a Bearer token.
export const API_BASE = (import.meta.env.VITE_API_BASE ?? "").replace(
  /\/+$/,
  "",
);
export const apiUrl = (path: string) => `${API_BASE}/api${path}`;
export function wsUrl(path: string) {
  const u = new URL(apiUrl(path), location.href);
  u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
  return u.toString();
}
const TOKEN_KEY = "camerahub.token";
export const tokenStore = {
  get: () => (API_BASE ? readStorage(TOKEN_KEY) : null),
  set: (t: string) => {
    if (API_BASE) writeStorage(TOKEN_KEY, t);
  },
  clear: () => writeStorage(TOKEN_KEY, null),
};
let unauthorized: () => void = () => {};
export const onUnauthorized = (fn: () => void) => {
  unauthorized = fn;
};
export function authHeaders(): Record<string, string> {
  const t = tokenStore.get();
  return t ? { Authorization: `Bearer ${t}` } : {};
}
async function request(
  path: string,
  body?: unknown,
  method?: string,
  signal?: AbortSignal,
): Promise<Response> {
  let r: Response;
  try {
    r = await fetch(apiUrl(path), {
      method: method || (body !== undefined ? "POST" : "GET"),
      credentials: "include",
      signal,
      headers: {
        ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
        ...authHeaders(),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    throw new Error("เชื่อมต่อเครื่องกลางไม่ได้");
  }
  if (r.status === 401) {
    tokenStore.clear();
    unauthorized();
  }
  if (!r.ok) {
    const data = await r.json().catch(() => ({}));
    throw new Error(data.error || `ดำเนินการไม่สำเร็จ (${r.status})`);
  }
  return r;
}
export async function api<T>(
  path: string,
  body?: unknown,
  method?: string,
  signal?: AbortSignal,
): Promise<T> {
  const r = await request(path, body, method, signal);
  if (r.status === 204 || !r.headers.get("content-type")?.includes("json"))
    return undefined as T;
  return r.json() as Promise<T>;
}
export const apiBlob = (path: string, body: unknown) =>
  request(path, body).then((r) => r.blob());
export async function signIn(path: "/login" | "/setup", body: unknown) {
  const out = await api<{ token?: string } | undefined>(path, {
    ...(body as object),
    ...(API_BASE ? { token: true } : {}),
  });
  if (out?.token) tokenStore.set(out.token);
}
export const statusText: Record<string, string> = {
  online: "พร้อมดู",
  pending: "รอตรวจภาพ",
  offline: "ออฟไลน์",
  auth_expired: "รอยืนยันบัญชี",
  error: "เชื่อมต่อไม่ได้",
  needs_credentials: "รอบัญชีกล้อง",
};
export const statusLabel = (s: string) => statusText[s] || "รอตรวจสอบ";
export const usable = (c: Camera) =>
  !["offline", "auth_expired", "needs_credentials"].includes(c.status);
