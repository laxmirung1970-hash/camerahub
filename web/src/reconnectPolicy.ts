export const ACCOUNT_PROBLEM = /(?:auth|401|403|credential|password)/i;

export function reconnectDelay(attempt: number, id: string): number {
  const jitter =
    Array.from(id).reduce((n, char) => n + char.charCodeAt(0), 0) % 3000;
  return (
    Math.min(60_000, 10_000 * 2 ** Math.min(Math.max(attempt, 0), 3)) + jitter
  );
}
export function requiresAccountFix(problem: string): boolean {
  return ACCOUNT_PROBLEM.test(problem);
}
