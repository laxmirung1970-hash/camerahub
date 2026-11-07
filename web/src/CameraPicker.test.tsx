// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { Camera } from "./api";
import { CameraPicker } from "./CameraPicker";

afterEach(cleanup);

const camera = (id: string, name: string, group: string): Camera => ({
  id,
  vendor: "xiaomi",
  ref: "",
  name,
  group,
  model: "C300",
  host: "",
  favorite: false,
  status: "online",
  audio: true,
  ptz: false,
  verification: "stream_only",
  audioVerified: false,
  ptzVerified: false,
});
const cams = [camera("a", "หน้าบ้าน", "ชั้นล่าง"), camera("b", "ครัว", "ชั้นล่าง"), camera("c", "ห้องนอน", "ชั้นบน")];

function mount(onEdit = vi.fn(async () => {}), onEditing = vi.fn()) {
  render(
    <CameraPicker
      cameras={cams}
      groups={["ชั้นล่าง", "ชั้นบน"]}
      selected={[]}
      showing={[]}
      owner
      ordering={false}
      bridgeReady
      onShowPage={() => {}}
      onToggle={() => {}}
      onFocus={() => {}}
      onEdit={onEdit}
      onEditing={onEditing}
      onMoveTo={() => {}}
    />,
  );
  return { onEdit, onEditing };
}

it("renames a camera and moves it to a known group from the pencil editor", async () => {
  const { onEdit, onEditing } = mount();
  expect(screen.queryByRole("form", { name: /แก้ไข/ })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "แก้ชื่อและกลุ่ม ครัว" }));
  await waitFor(() => expect(onEditing).toHaveBeenLastCalledWith(true));
  const form = screen.getByRole("form", { name: "แก้ไข ครัว" });
  expect(form).toBeTruthy();
  fireEvent.change(screen.getByLabelText("ชื่อกล้อง"), { target: { value: " ครัวหลัง " } });
  // Chips offer the other groups; the camera's own group is not repeated.
  expect(screen.queryByRole("button", { name: "ชั้นล่าง" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "ชั้นบน" }));
  fireEvent.submit(form);
  await waitFor(() => expect(onEdit).toHaveBeenCalledWith(cams[1], { name: "ครัวหลัง", group: "ชั้นบน" }));
  await waitFor(() => expect(screen.queryByRole("form", { name: /แก้ไข/ })).toBeNull());
  expect(onEditing).toHaveBeenLastCalledWith(false);
});

it("refuses an empty name, keeps the editor open on save errors, and closes on Escape", async () => {
  const onEdit = vi.fn(async () => {
    throw new Error("เครื่องกลางไม่ตอบ");
  });
  mount(onEdit);
  fireEvent.click(screen.getByRole("button", { name: "แก้ชื่อและกลุ่ม หน้าบ้าน" }));
  const form = screen.getByRole("form", { name: "แก้ไข หน้าบ้าน" });
  fireEvent.change(screen.getByLabelText("ชื่อกล้อง"), { target: { value: "   " } });
  fireEvent.submit(form);
  expect(screen.getByRole("alert").textContent).toBe("ชื่อกล้องห้ามว่าง");
  expect(onEdit).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText("ชื่อกล้อง"), { target: { value: "ประตูหน้า" } });
  fireEvent.submit(form);
  await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("เครื่องกลางไม่ตอบ"));
  expect(screen.getByRole("form", { name: "แก้ไข หน้าบ้าน" })).toBeTruthy();
  fireEvent.keyDown(screen.getByLabelText("ชื่อกล้อง"), { key: "Escape" });
  expect(screen.queryByRole("form", { name: /แก้ไข/ })).toBeNull();
});

it("shows the group on each row and hides the pencil for viewers", () => {
  const { container } = render(
    <CameraPicker
      cameras={cams}
      groups={[]}
      selected={[]}
      showing={[]}
      owner={false}
      ordering={false}
      bridgeReady
      onShowPage={() => {}}
      onToggle={() => {}}
      onFocus={() => {}}
      onEditing={() => {}}
      onMoveTo={() => {}}
    />,
  );
  expect([...container.querySelectorAll(".picker-group")].map((el) => el.textContent?.trim())).toEqual(["ชั้นล่าง ·", "ชั้นล่าง ·", "ชั้นบน ·"]);
  expect(screen.queryByRole("button", { name: /แก้ชื่อและกลุ่ม/ })).toBeNull();
});

it("starts a new page for each group and names the group in the page header", () => {
  const onShowPage = vi.fn();
  render(
    <CameraPicker
      cameras={cams}
      groups={[]}
      selected={[]}
      showing={[]}
      owner={false}
      ordering={false}
      bridgeReady
      onShowPage={onShowPage}
      onToggle={() => {}}
      onFocus={() => {}}
      onEditing={() => {}}
      onMoveTo={() => {}}
    />,
  );
  // Three cameras in two groups: ชั้นล่าง (2) then ชั้นบน (1) = two pages, not one.
  const headers = screen.getAllByText(/หน้า \d/).map((el) => el.textContent?.replace(/\s+/g, " "));
  expect(headers).toEqual(["ชั้นล่าง · หน้า 1 / 2", "ชั้นบน · หน้า 2 / 2"]);
  fireEvent.click(screen.getAllByRole("button", { name: "แสดง" })[1]);
  expect(onShowPage).toHaveBeenCalledWith(1);
});
