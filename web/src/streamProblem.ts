import { ACCOUNT_PROBLEM } from "./reconnectPolicy";

export type StreamProblem = { title: string; help: string };
// Only show these fixed explanations. Upstream errors can contain private URLs.
export function streamProblem(category: string): StreamProblem {
  const value = category.toLowerCase();
  if (ACCOUNT_PROBLEM.test(value))
    return {
      title: "ยืนยันบัญชีกล้องไม่ได้",
      help: "ตรวจ Camera Account หรือยืนยันบัญชีในหน้าการเชื่อมต่อ",
    };
  if (/codec|decode|bufferadd|unsupported|incompatible/.test(value))
    return {
      title: "รูปแบบภาพไม่รองรับบนอุปกรณ์นี้",
      help: "ลองประหยัดข้อมูล หรือเปิดโหมดภาพ H.264 ในรายละเอียดกล้อง",
    };
  if (/stalled/.test(value))
    return {
      title: "ภาพหยุดหรือการเชื่อมต่อขาด",
      help: "ตรวจไฟและ Wi-Fi ของกล้อง แล้วลองเชื่อมต่อใหม่",
    };
  if (/timeout|deadline|no.video|refused|404|not.found|offline/.test(value))
    return {
      title: "ติดต่อกล้องหรือรับภาพไม่ได้",
      help: "ตรวจการเชื่อมต่อของกล้องและเครื่องกลาง รวมถึงไฟ Wi-Fi และ IP กล้อง",
    };
  if (/gateway|websocket|server|502|503/.test(value))
    return {
      title: "ตัวกลางวิดีโอเชื่อมต่อไม่ได้",
      help: "ตรวจว่าเครื่องกลางและระบบวิดีโอยังเปิดทำงาน",
    };
  return {
    title: "กล้องยังส่งภาพมาไม่สำเร็จ",
    help: "ลองเชื่อมต่อใหม่ หรือเปิดแอปของกล้องเพื่อตรวจสถานะ",
  };
}
