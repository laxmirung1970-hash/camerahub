import { defineConfig, loadEnv } from "vite";
import type { Plugin } from "vite";
import react from "@vitejs/plugin-react";

// The lite (family, view-only) build shares index.html but needs its own title and manifest.
function liteHtml(): Plugin {
  return {
    name: "camerahub-lite-html",
    transformIndexHtml(html) {
      return html
        .replace(/<title>[^<]*<\/title>/, "<title>ห้องอาม่า</title>")
        .replace('href="/manifest.webmanifest"', 'href="/manifest-lite.webmanifest"')
        .replace(/(<meta\s+name="description"\s+content=")[^"]*(")/, "$1กล้องห้องอาม่า ดูอย่างเดียว$2");
    },
  };
}
export default defineConfig(({ mode }) => {
  const env = { ...loadEnv(mode, process.cwd(), "VITE_"), ...process.env };
  return {
    plugins: [react(), ...(env.VITE_VARIANT === "lite" ? [liteHtml()] : [])],
    server: { proxy: { "/api": { target: "http://127.0.0.1:17800", ws: true } } },
    build: { sourcemap: false },
  };
});
