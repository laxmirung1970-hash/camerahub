import { useCallback, useEffect, useState } from "react";
import type { Bootstrap, Camera, Connection, System } from "../api";
import { api } from "../api";
import { Modal } from "../ui";
import { ConnectionsTab } from "./ConnectionsTab";
import { DevicesTab } from "./DevicesTab";
import { SystemTab } from "./SystemTab";

export type Notify = (s: string, kind?: "ok" | "error") => void;
/** Paired TV or family share link; share rows carry their camera allowlist. */
export interface Device {
  id: string;
  name: string;
  kind: "tv" | "share";
  expires?: number;
  cameras?: string[];
}
export interface SettingsData {
  system?: System;
  connections: Connection[];
  devices: Device[];
  pending: { code: string; name: string }[];
}
export interface TabProps {
  boot: Bootstrap;
  cameras: Camera[];
  data: SettingsData;
  busy: boolean;
  action: (fn: () => Promise<void>) => Promise<void>;
  notify: Notify;
  refresh: () => Promise<void>;
}
const TABS = [
  ["connections", "เชื่อมกล้อง"],
  ["devices", "ทีวีและอุปกรณ์"],
  ["system", "ระบบและสำรอง"],
] as const;

export function SettingsModal({
  tab,
  setTab,
  boot,
  cameras,
  onClose,
  notify,
  refresh,
}: {
  tab: string;
  setTab: (s: string) => void;
  boot: Bootstrap;
  cameras: Camera[];
  onClose: () => void;
  notify: Notify;
  refresh: () => Promise<void>;
}) {
  const [data, setData] = useState<SettingsData>({ connections: [], devices: [], pending: [] });
  const [busy, setBusy] = useState(false);
  const reload = useCallback(() => {
    if (boot.kind !== "owner") return;
    void Promise.all([
      api<System>("/system").catch(() => undefined),
      api<Connection[]>("/connections").catch(() => []),
      api<Device[]>("/devices").catch(() => []),
      api<{ code: string; name: string }[]>("/pair/pending").catch(() => []),
    ]).then(([system, connections, devices, pending]) =>
      setData({ system, connections: connections ?? [], devices: devices ?? [], pending: pending ?? [] }),
    );
  }, [boot.kind]);
  useEffect(() => {
    reload();
    const t = setInterval(reload, 5000);
    return () => clearInterval(t);
  }, [reload]);
  const action = async (fn: () => Promise<void>) => {
    setBusy(true);
    try {
      await fn();
      reload();
    } catch (e) {
      notify((e as Error).message, "error");
    } finally {
      setBusy(false);
    }
  };
  const props: TabProps = { boot, cameras, data, busy, action, notify, refresh };
  return (
    <Modal title="ตั้งค่า" onClose={onClose} wide>
      <div className="tabs" role="tablist">
        {TABS.map(([id, label]) => (
          <button key={id} role="tab" aria-selected={tab === id} onClick={() => setTab(id)}>
            {label}
          </button>
        ))}
      </div>
      {tab === "connections" && <ConnectionsTab {...props} />}
      {tab === "devices" && <DevicesTab {...props} />}
      {tab === "system" && <SystemTab {...props} />}
    </Modal>
  );
}
