import { useEffect, useState } from "react";

const format = new Intl.DateTimeFormat("th-TH", {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  timeZone: "Asia/Bangkok",
  hour12: false,
});
/** Ticks on its own so the rest of the app does not re-render every second. */
export function Clock() {
  const [now, setNow] = useState(() => format.format(new Date()));
  useEffect(() => {
    const t = setInterval(() => setNow(format.format(new Date())), 1000);
    return () => clearInterval(t);
  }, []);
  return (
    <time className="clock" dateTime={now}>
      {now}
    </time>
  );
}
