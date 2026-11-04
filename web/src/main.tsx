import React from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import LiteApp from "./lite/LiteApp";
import "./styles.css";

// VITE_VARIANT=lite builds the view-only family app from the same package.
const Root = import.meta.env.VITE_VARIANT === "lite" ? LiteApp : App;
// TV browsers get larger type, controls and focus ring; ?tv=1 forces it.
if (/\b(SmartTV|SMART-TV|Tizen|Web0S|BRAVIA|Android TV|AFT)\b/i.test(navigator.userAgent) || new URLSearchParams(location.search).has("tv"))
  document.body.dataset.tv = "1";
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <Root />
  </React.StrictMode>,
);
if ("serviceWorker" in navigator && import.meta.env.PROD) void navigator.serviceWorker.register("/sw.js");
