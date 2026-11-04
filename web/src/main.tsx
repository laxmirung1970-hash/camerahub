import React from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import "./styles.css";

// TV browsers get larger type, controls and focus ring; ?tv=1 forces it.
if (/\b(SmartTV|SMART-TV|Tizen|Web0S|BRAVIA|Android TV|AFT)\b/i.test(navigator.userAgent) || new URLSearchParams(location.search).has("tv"))
  document.body.dataset.tv = "1";
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
if ("serviceWorker" in navigator && import.meta.env.PROD) void navigator.serviceWorker.register("/sw.js");
