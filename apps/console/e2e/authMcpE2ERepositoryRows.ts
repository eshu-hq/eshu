// authMcpE2ERepositoryRows.ts — reads the repository list out of a
// list_indexed_repositories tools/call result.
//
// The MCP dispatcher asks the API for the truth envelope
// (`Accept: application/eshu.envelope+json`, go/internal/mcp/dispatch.go), the
// API answers `{data, truth, error}` (go/internal/querycontract/http.go), and
// the MCP server forwards that envelope as `structuredContent`
// (go/internal/mcp/server.go). The repositories therefore live at
// `structuredContent.data.repositories`. Reading `structuredContent.repositories`
// finds nothing on every response, which turned "the scoped token sees zero
// repositories" into a check that could not fail.

// RepositoryRow is one repository the tool listed.
export interface RepositoryRow {
  readonly id?: string;
}

// RepositoryList is the parsed list plus the tool's reported total.
export interface RepositoryList {
  readonly rows: readonly RepositoryRow[];
  readonly total: number | undefined;
}

// repositoryListFromEnvelope extracts `data.repositories` from a tools/call
// structuredContent value. It fails closed: any shape that is not an envelope
// carrying a `data.repositories` array throws, so a payload change can never
// read as "zero repositories". An envelope error (`error` set) also throws.
export function repositoryListFromEnvelope(structured: unknown): RepositoryList {
  if (structured === null || typeof structured !== "object") {
    throw new Error(`list_indexed_repositories structuredContent is not an object: ${JSON.stringify(structured)}`);
  }
  const envelope = structured as { data?: unknown; error?: unknown };
  if (envelope.error !== undefined && envelope.error !== null) {
    throw new Error(`list_indexed_repositories envelope carries an error: ${JSON.stringify(envelope.error)}`);
  }
  const data = envelope.data;
  if (data === null || typeof data !== "object") {
    throw new Error(`list_indexed_repositories structuredContent has no envelope data object: ${JSON.stringify(structured)}`);
  }
  const { repositories, total } = data as { repositories?: unknown; total?: unknown };
  if (!Array.isArray(repositories)) {
    throw new Error(`list_indexed_repositories envelope data has no repositories array: ${JSON.stringify(data)}`);
  }
  return { rows: repositories as readonly RepositoryRow[], total: typeof total === "number" ? total : undefined };
}

// assertEmptyGrantRepositoryList asserts a scoped credential with an EMPTY
// repository grant saw no repositories, and returns the run-log detail. It
// throws when any row came back (a scope escape) and, through
// repositoryListFromEnvelope, when the shape is not recognised.
export function assertEmptyGrantRepositoryList(structured: unknown, who: string): string {
  const { rows, total } = repositoryListFromEnvelope(structured);
  if (rows.length !== 0) {
    throw new Error(
      `scope-escalation: ${who} saw ${rows.length} repository row(s) despite an empty grant — the seeded node must be filtered out. ids: ${JSON.stringify(rows.map((r) => r.id))}`,
    );
  }
  return `${who} (empty grant) correctly saw 0 repositories (total=${total ?? 0})`;
}
