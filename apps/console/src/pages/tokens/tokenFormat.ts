// pages/tokens/tokenFormat.ts — tiny date-formatting helpers shared between
// ProfilePage.tsx (sessions + tokens) and the tokens/ components (issue
// #5164 extraction), so both render timestamps identically without a
// circular import between ProfilePage and TokensSection.
export function fmt(iso: string | undefined): string {
  if (!iso) return "—";
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return "—";
  }
}

// isExpired reports whether a token's expiry has passed. An expired-but-not-
// revoked token must not be labeled "active" — that would imply it is still
// usable. Tokens with no expiry never expire.
export function isExpired(iso: string | undefined): boolean {
  if (!iso) return false;
  const ms = new Date(iso).getTime();
  return Number.isFinite(ms) && ms < Date.now();
}
