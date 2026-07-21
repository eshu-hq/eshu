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
// as failure; see pollForGovernanceAuditEvent below.
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

// pollForGovernanceAuditEvent polls findLatestGovernanceAuditEvent until a row
// newer than sinceIso appears or deadlineMs elapses. The F-9 part-1 allowed-read
// audit path is async (governanceauditasync.AsyncAppender): the event is not
// guaranteed to be durable the instant the MCP response returns, so this bounds
// the wait instead of asserting on a single synchronous read (design §9
// decision: "Shape-A asserts it via psql with a BOUNDED poll <=10s since
// emission is async").
export async function pollForGovernanceAuditEvent(
  repoRoot: string,
  project: string,
  filter: { readonly eventType: string; readonly decision: string; readonly reasonCode?: string },
  sinceIso: string,
  deadlineMs: number,
): Promise<GovernanceAuditEventRow> {
  const sinceMs = Date.parse(sinceIso);
  const start = Date.now();
  let lastMiss = "no matching row found yet";
  while (Date.now() - start < deadlineMs) {
    const row = await findLatestGovernanceAuditEvent(repoRoot, project, filter);
    if (row) {
      const occurredMs = Date.parse(row.occurredAt);
      if (!Number.isNaN(occurredMs) && occurredMs >= sinceMs) {
        return row;
      }
      lastMiss = `latest matching row is older than the request (occurred_at=${row.occurredAt}, since=${sinceIso})`;
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(
    `governance_audit_events: no ${filter.eventType}/${filter.decision}${filter.reasonCode ? `/${filter.reasonCode}` : ""} row observed within ${deadlineMs}ms: ${lastMiss}`,
  );
}
