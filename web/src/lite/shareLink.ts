// A share link carries the view-only token in the URL fragment so it never
// reaches the server or a proxy log: https://<lite origin>/#<token>.
const TOKEN = /^[A-Za-z0-9_-]{32,}$/;

export function takeSecretFromHash(hash: string): string | null {
  const raw = hash.startsWith("#") ? hash.slice(1) : hash;
  return TOKEN.test(raw) ? raw : null;
}
export const buildShareLink = (origin: string, token: string) =>
  `${origin.replace(/\/+$/, "")}/#${token}`;
