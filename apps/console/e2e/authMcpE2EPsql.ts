// authMcpE2EPsql.ts — shared psql-exec helper for the MCP-identity E2E suite
// (F-9, issue #5170). Every shape module that needs to read the
// governance_audit_events table directly (shape A's audit-subject assertion,
// the leakage module's distinct-reason-code proof, the cross-tenant seed)
// goes through this one function rather than re-deriving the
// `docker compose exec -T postgres psql` invocation each time. Pattern
// cloned from authE2ELocalMemberFlow.ts's psql helpers (that file's
// createLocalInvitationDirect/seedMustChangePassword), which in turn documents
// why direct psql is used: no supported product API exists for this kind of
// seed/read (see f9-design.md §9 decision 1).
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { authE2EComposeArgs } from "./authE2EComposeArgs";

const execFileAsync = promisify(execFile);

// runPsql executes one SQL statement against the e2e stack's Postgres via the
// compose-managed container's own bundled psql (postgres:18-alpine), exactly
// like authE2ELocalMemberFlow.ts's helpers. `-tA` strips headers/alignment so
// tuples-only output is trivial to split on newlines/pipe. Returns raw stdout;
// callers parse it (single-column callers split on "\n", multi-column callers
// split each row on "|" first).
export async function runPsql(repoRoot: string, project: string, sql: string): Promise<string> {
  const composeArgs = authE2EComposeArgs(project);
  const psqlArgs = ["exec", "-T", "postgres", "psql", "-U", "eshu", "-d", "eshu", "-tA", "-c", sql];
  const { stdout } = await execFileAsync("docker", [...composeArgs, ...psqlArgs], {
    cwd: repoRoot,
    maxBuffer: 16 * 1024 * 1024,
  });
  return stdout;
}

// splitPsqlRows splits `-tA` tuples-only output into non-empty rows, each
// itself split on the default psql field separator "|".
export function splitPsqlRows(stdout: string): string[][] {
  return stdout
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .map((line) => line.split("|"));
}

export interface GovernanceAuditFilter {
  readonly eventType: string;
  readonly decision: string;
  readonly reasonCode?: string;
}

// governanceAuditWhereClause builds the shared WHERE fragment for a filter.
function governanceAuditWhereClause(filter: GovernanceAuditFilter): string {
  const reasonClause = filter.reasonCode
    ? ` AND reason_code = '${filter.reasonCode.replace(/'/g, "''")}'`
    : "";
  return (
    `WHERE event_type = '${filter.eventType.replace(/'/g, "''")}' ` +
    `AND decision = '${filter.decision.replace(/'/g, "''")}'${reasonClause}`
  );
}

// countGovernanceAuditEvents returns the number of matching rows. Callers
// capture a BASELINE count before an action, then recount after it — a
// DB-state-vs-DB-state comparison that is immune to host/container
// wall-clock skew, unlike a timestamp comparison. Callers asserting absence
// wait out the retired async sink's flush window before the recount (see
// shape A's no-emission assertion).
export async function countGovernanceAuditEvents(
  repoRoot: string,
  project: string,
  filter: GovernanceAuditFilter,
): Promise<number> {
  const sql = `SELECT COUNT(*) FROM governance_audit_events ${governanceAuditWhereClause(filter)};`;
  const stdout = await runPsql(repoRoot, project, sql);
  const rows = splitPsqlRows(stdout);
  const n = rows.length > 0 ? Number.parseInt(rows[0]![0] ?? "", 10) : NaN;
  if (Number.isNaN(n)) {
    throw new Error(`countGovernanceAuditEvents: unparseable COUNT output: ${JSON.stringify(stdout)}`);
  }
  return n;
}
