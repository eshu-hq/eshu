import type { DeadCodePage } from "../api/deadCode";
import type { FindingRow } from "../console/types";

export interface DeadCodeRepositoryGroup {
  readonly key: string;
  readonly repository: string;
  readonly repositoryId: string | null;
  readonly rows: readonly FindingRow[];
}

export function groupDeadCodeByRepository(
  rows: readonly FindingRow[],
  repositoryNames: ReadonlyMap<string, string> = new Map(),
): readonly DeadCodeRepositoryGroup[] {
  const groups = new Map<
    string,
    {
      readonly label: string;
      readonly repositoryId: string | null;
      readonly rows: FindingRow[];
    }
  >();
  for (const row of rows) {
    const repositoryId = row.repoId?.trim() || null;
    const fallbackLabel = repositoryFallbackLabel(row.entity);
    const key = repositoryId ?? `unresolved:${fallbackLabel}`;
    const label = repositoryId
      ? (repositoryNames.get(repositoryId) ?? fallbackLabel)
      : fallbackLabel;
    const group = groups.get(key);
    groups.set(key, {
      label,
      repositoryId,
      rows: group ? [...group.rows, row] : [row],
    });
  }
  return [...groups.entries()]
    .map(([key, group]) => ({
      key,
      repository: group.label,
      repositoryId: group.repositoryId,
      rows: group.rows,
    }))
    .sort((a, b) => b.rows.length - a.rows.length || a.repository.localeCompare(b.repository));
}

export function deadCodeLanguages(
  page: DeadCodePage | null,
  rows: readonly FindingRow[],
): readonly string[] {
  const maturity = page?.analysis.dead_code_language_maturity;
  const supported =
    maturity !== null && typeof maturity === "object" && !Array.isArray(maturity)
      ? Object.keys(maturity)
      : [];
  return uniqueStrings([
    ...supported,
    ...rows.map((row) => row.language?.trim() ?? "").filter(Boolean),
  ]);
}

export function deadCodeScanLabel(page: DeadCodePage | null, live: boolean): string {
  if (!page) return live ? "direct live scan" : "snapshot";
  const qualifiers: string[] = [];
  if (page.displayTruncated) qualifiers.push(`display limited to ${page.limit} candidates`);
  if (page.candidateScanTruncated) qualifiers.push("candidate scan window incomplete");
  if (page.truncated && qualifiers.length === 0) qualifiers.push("response truncated");
  if (qualifiers.length === 0) qualifiers.push("complete for current scope");
  return `${page.limit} candidate limit · ${qualifiers.join(" · ")}`;
}

export function uniqueStrings(values: readonly string[]): readonly string[] {
  return [...new Set(values)].sort((a, b) => a.localeCompare(b));
}

export function matchesDeadCodeQuery(
  finding: FindingRow,
  query: string,
  repositoryName?: string,
): boolean {
  const normalized = query.trim().toLowerCase();
  if (normalized === "") return true;
  return [
    finding.title,
    finding.entity,
    finding.detail,
    finding.filePath ?? "",
    finding.language ?? "",
    repositoryName ?? "",
  ]
    .join(" ")
    .toLowerCase()
    .includes(normalized);
}

export function symbolFromFinding(finding: FindingRow): string {
  return finding.title.replace(/^Unreferenced symbol\s+/i, "").trim() || finding.title;
}

export function classificationFromFinding(finding: FindingRow): string {
  return finding.classification ?? finding.detail.split(" · ")[1] ?? "";
}

export function kindFromFinding(finding: FindingRow): string {
  return finding.labels?.[0]?.toLowerCase() ?? "symbol";
}

export function locationFromFinding(finding: FindingRow): string {
  const path = finding.filePath ?? finding.detail.split(" · ")[0] ?? "source path unavailable";
  return finding.startLine ? `${path}:${finding.startLine}` : path;
}

export function sourceHref(finding: FindingRow): string | null {
  const filePath = finding.filePath;
  const repository = finding.repoId?.trim();
  if (!filePath || !repository) return null;
  const params = new URLSearchParams({ path: filePath });
  if (finding.startLine !== undefined) params.set("lineStart", String(finding.startLine));
  if (finding.endLine !== undefined) params.set("lineEnd", String(finding.endLine));
  return `/repositories/${encodeURIComponent(repository)}/source?${params.toString()}`;
}

export function codeGraphHref(finding: FindingRow): string {
  return `/code-graph?candidate=${encodeURIComponent(finding.id)}`;
}

export function locFromFinding(finding: FindingRow): number {
  if (finding.startLine && finding.endLine && finding.endLine >= finding.startLine) {
    return finding.endLine - finding.startLine + 1;
  }
  return 0;
}

export function deadCodeRepositoryHref(repositoryId: string, language?: string): string {
  const params = new URLSearchParams({ repo_id: repositoryId });
  const trimmedLanguage = language?.trim();
  if (trimmedLanguage) params.set("language", trimmedLanguage);
  return `/dead-code?${params.toString()}`;
}

function repositoryFallbackLabel(entity: string): string {
  const label = entity.trim();
  return label && label.toLowerCase() !== "repository" ? label : "unresolved repository";
}
