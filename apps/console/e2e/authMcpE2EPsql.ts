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

const execFileAsync = promisify(execFile);

// runPsql executes one SQL statement against the e2e stack's Postgres via the
// compose-managed container's own bundled psql (postgres:18-alpine), exactly
// like authE2ELocalMemberFlow.ts's helpers. `-tA` strips headers/alignment so
// tuples-only output is trivial to split on newlines/pipe. Returns raw stdout;
// callers parse it (single-column callers split on "\n", multi-column callers
// split each row on "|" first).
export async function runPsql(repoRoot: string, project: string, sql: string): Promise<string> {
  const composeArgs = ["compose", "-p", project, "-f", "docker-compose.e2e.yaml"];
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

export interface GovernanceAuditEventRow {
  readonly eventType: string;
  readonly actorClass: string;
  readonly actorIdHash: string;
  readonly decision: string;
  readonly reasonCode: string;
  readonly tenantId: string;
  readonly occurredAt: string;
}

// findLatestGovernanceAuditEvent reads the most recent governance_audit_events
// row matching eventType/decision (and, when provided, reasonCode), ordered by
// occurred_at DESC. Returns null when no matching row exists yet — callers
// that need "eventually appears" semantics (the async allowed-read audit path,
// F-9 part 1) wrap this in a bounded poll rather than treating a single miss
// as failure; see pollForNewGovernanceAuditEvent below.
export async function findLatestGovernanceAuditEvent(
  repoRoot: string,
  project: string,
  filter: { readonly eventType: string; readonly decision: string; readonly reasonCode?: string },
): Promise<GovernanceAuditEventRow | null> {
  const reasonClause = filter.reasonCode
    ? ` AND reason_code = '${filter.reasonCode.replace(/'/g, "''")}'`
    : "";
  const sql =
    "SELECT event_type, actor_class, COALESCE(actor_id_hash, ''), decision, reason_code, " +
    "COALESCE(tenant_id, ''), occurred_at FROM governance_audit_events " +
    `WHERE event_type = '${filter.eventType.replace(/'/g, "''")}' AND decision = '${filter.decision.replace(/'/g, "''")}'${reasonClause} ` +
    "ORDER BY occurred_at DESC LIMIT 1;";
  const stdout = await runPsql(repoRoot, project, sql);
  const rows = splitPsqlRows(stdout);
  if (rows.length === 0) {
    return null;
  }
  const [eventType, actorClass, actorIdHash, decision, reasonCode, tenantId, occurredAt] = rows[0]!;
  return {
    eventType: eventType ?? "",
    actorClass: actorClass ?? "",
    actorIdHash: actorIdHash ?? "",
    decision: decision ?? "",
    reasonCode: reasonCode ?? "",
    tenantId: tenantId ?? "",
    occurredAt: occurredAt ?? "",
  };
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
// capture a BASELINE count before an action, then poll until the count
// increases (pollForNewGovernanceAuditEvent) — a DB-state-vs-DB-state
// comparison that is immune to host/container wall-clock skew, unlike a
// timestamp comparison.
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

// pollForNewGovernanceAuditEvent polls until the matching-row COUNT exceeds
// baselineCount (a NEW event has landed) or deadlineMs elapses, then returns
// the latest matching row. The F-9 part-1 allowed-read audit path is async
// (governanceauditasync.AsyncAppender): the event is not durable the instant
// the MCP response returns, so this bounds the wait (design §9: "Shape-A
// asserts it via psql with a BOUNDED poll <=10s since emission is async").
//
// It counts new rows rather than comparing occurred_at against a caller-side
// timestamp: the runner (host clock) and the mcp-server (container clock) can
// skew by tens of milliseconds, so an event that genuinely landed AFTER the
// request can carry an occurred_at that reads as slightly BEFORE a host-captured
// `since` — a real cross-clock flake this design avoids by construction.
export async function pollForNewGovernanceAuditEvent(
  repoRoot: string,
  project: string,
  filter: GovernanceAuditFilter,
  baselineCount: number,
  deadlineMs: number,
): Promise<GovernanceAuditEventRow> {
  const start = Date.now();
  let lastCount = baselineCount;
  while (Date.now() - start < deadlineMs) {
    lastCount = await countGovernanceAuditEvents(repoRoot, project, filter);
    if (lastCount > baselineCount) {
      const row = await findLatestGovernanceAuditEvent(repoRoot, project, filter);
      if (row) {
        return row;
      }
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(
    `governance_audit_events: no NEW ${filter.eventType}/${filter.decision}` +
      `${filter.reasonCode ? `/${filter.reasonCode}` : ""} row within ${deadlineMs}ms ` +
      `(baseline count ${baselineCount}, last observed ${lastCount})`,
  );
}
