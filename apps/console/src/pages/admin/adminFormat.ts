// pages/admin/adminFormat.ts
// Small shared formatting helpers for the admin panels. Kept in one place so
// every panel renders timestamps and empty values identically.

// fmt renders an ISO timestamp in the local locale, or an em dash for an
// absent or unparseable value. Never throws.
export function fmt(iso: string | undefined): string {
  if (!iso) return "—";
  try {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString();
  } catch {
    return "—";
  }
}

// dash renders a value or an em dash when it is absent/empty.
export function dash(value: string | undefined): string {
  return value && value.length > 0 ? value : "—";
}

// truncatedNote renders the "showing first N" hint when a list response was
// truncated by the backend, or an empty string otherwise.
export function truncatedNote(truncated: boolean, shown: number): string {
  return truncated ? `Showing first ${shown} (results truncated by the server).` : "";
}
