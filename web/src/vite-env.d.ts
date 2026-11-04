/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_BASE?: string;
  /** "lite" builds the view-only family app (LiteApp) instead of the full UI. */
  readonly VITE_VARIANT?: string;
  /** Origin of the lite deployment; the main app uses it to print share links. */
  readonly VITE_SHARE_ORIGIN?: string;
}
