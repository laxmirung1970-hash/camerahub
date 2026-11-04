// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { Bootstrap, Camera } from "../api";
import { DevicesTab } from "./DevicesTab";
import type { TabProps } from "./SettingsModal";

afterEach(cleanup);

const camera = (id: string, name: string): Camera => ({
  id,
  vendor: "xiaomi",
  ref: "",
  name,
  group: "ห้องอาม่า",
  model: "C300",
  host: "",
  favorite: false,
  status: "online",
  audio: true,
  ptz: true,
  verification: "stream_only",
  audioVerified: false,
  ptzVerified: false,
});
const boot: Bootstrap = { mode: "demo", needsSetup: false, authenticated: true, name: "o", kind: "owner", version: "t", webRTC: false };

it("creates a share link from the picked cameras and shows the token once", async () => {
  const token = "fixture_token-0123456789abcdefABCDEF0123456789";
  const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
    expect(JSON.parse(String(init?.body))).toEqual({ name: "ห้องอาม่า", cameras: ["xiaomi-a", "xiaomi-b"] });
    return new Response(JSON.stringify({ token }), { status: 201, headers: { "content-type": "application/json" } });
  });
  vi.stubGlobal("fetch", fetchMock);
  const notify = vi.fn();
  const props: TabProps = {
    boot,
    cameras: [camera("xiaomi-a", "อาม่า 1"), camera("xiaomi-b", "อาม่า 2"), camera("tapo-c", "หน้าบ้าน")],
    data: { connections: [], devices: [{ id: "h1", name: "ทีวีห้องนั่งเล่น", kind: "tv" }], pending: [] },
    busy: false,
    action: async (fn) => fn(),
    notify,
    refresh: async () => {},
  };
  render(<DevicesTab {...props} />);
  fireEvent.change(screen.getByLabelText("ชื่อลิงก์แชร์"), { target: { value: "ห้องอาม่า" } });
  const create = screen.getByText("สร้างลิงก์แชร์").closest("button")!;
  expect(create.hasAttribute("disabled")).toBe(true);
  fireEvent.click(screen.getByLabelText(/อาม่า 1/));
  fireEvent.click(screen.getByLabelText(/อาม่า 2/));
  expect(create.hasAttribute("disabled")).toBe(false);
  fireEvent.click(create);
  await waitFor(() => expect(screen.getByTestId("share-link").textContent).toContain(token));
  expect(fetchMock).toHaveBeenCalledTimes(1);
  expect(String(fetchMock.mock.calls[0][0])).toContain("/api/shares");
  expect(notify).toHaveBeenCalledWith("สร้างลิงก์แชร์แล้ว");
  vi.unstubAllGlobals();
});

it("lists share links with their camera count next to paired TVs", () => {
  render(
    <DevicesTab
      boot={boot}
      cameras={[]}
      data={{ connections: [], devices: [{ id: "h2", name: "ครอบครัว", kind: "share", cameras: ["a", "b"] }], pending: [] }}
      busy={false}
      action={async (fn) => fn()}
      notify={() => {}}
      refresh={async () => {}}
    />,
  );
  expect(screen.getByText("ครอบครัว")).toBeTruthy();
  expect(screen.getByText(/กล้อง 2 ตัว/)).toBeTruthy();
});
