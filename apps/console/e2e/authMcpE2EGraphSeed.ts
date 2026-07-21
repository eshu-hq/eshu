// authMcpE2EGraphSeed.ts — graph + compose-log helpers for the MCP-identity
// E2E suite's negative-leakage module (F-9, issue #5170, design §5).
//
// Why a graph seed at all: list_indexed_repositories (GET /api/v0/repositories)
// is GRAPH-backed in this stack — RepositoryHandler.Neo4j is wired
// (go/cmd/mcp-server/wiring_router.go), so the handler runs
// `MATCH (r:Repository) ...` against NornicDB, NOT the Postgres content store.
// A psql seed therefore cannot populate it (the design's "psql cross-tenant
// seed" wording predates that finding — see runAuthMcpE2E.ts's seed-step
// comment for the full adaptation). On a zero-corpus stack there is nothing
// to row-filter, so the scoped-vs-AllScopes visibility assertion would pass
// vacuously; this seeds exactly one Repository node so the assertion is real.
//
// The seed is additive and the whole stack is torn down after every run
// (scripts/run-auth-mcp-e2e.sh's `down -v`), so it never contaminates a later
// run — matching the psql-seed precedent (authE2ELocalMemberFlow.ts).
import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

// SEEDED_REPOSITORY_ID is the id of the single Repository node the suite
// seeds. AllScopes credentials must see it; a scoped (grant-empty) credential
// must not (design §5's non-vacuous row-filter proof).
export const SEEDED_REPOSITORY_ID = "e2e-seed-repo-default";

// seedGraphRepository CREATEs one Repository node in NornicDB over its
// Neo4j-compatible HTTP transaction endpoint (/db/nornic/tx/commit). Uses a
// plain single-label CREATE and reads it back with a plain
// `MATCH (r:Repository)` — deliberately NOT the backtick-quoted label or the
// label-disjunction shapes NornicDB mishandles (documented pitfalls), and the
// exact shape the AllScopes RepositoryHandler path runs. neo4j/change-me is
// the compose default (docker-compose.yaml NEO4J_PASSWORD).
export async function seedGraphRepository(nornicHttpBase: string, repoId: string): Promise<void> {
  const body = JSON.stringify({
    statements: [
      {
        statement: "CREATE (r:Repository {id: $id, name: $name}) RETURN r.id",
        parameters: { id: repoId, name: repoId },
      },
    ],
  });
  const res = await fetch(`${nornicHttpBase}/db/nornic/tx/commit`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      // Basic neo4j:change-me
      Authorization: "Basic bmVvNGo6Y2hhbmdlLW1l",
    },
    body,
  });
  if (!res.ok) {
    throw new Error(`seedGraphRepository: CREATE returned ${res.status}: ${await res.text()}`);
  }
  const parsed = (await res.json()) as { errors?: readonly unknown[]; results?: readonly unknown[] };
  if (parsed.errors && parsed.errors.length > 0) {
    throw new Error(`seedGraphRepository: NornicDB reported errors: ${JSON.stringify(parsed.errors)}`);
  }
  if (!parsed.results || parsed.results.length === 0) {
    throw new Error(`seedGraphRepository: CREATE returned no results: ${JSON.stringify(parsed)}`);
  }
}

// collectComposeServiceLogs returns one compose service's full stdout+stderr,
// the generic sibling of authE2ELeakage.ts's eshu-only collectApiContainerLogs
// (that helper hardcodes the "eshu" service; the leakage matrix needs
// mcp-server and mock-github too). Reads the whole log with a generous buffer
// so a non-empty read can be asserted by the caller.
export async function collectComposeServiceLogs(
  repoRoot: string,
  project: string,
  service: string,
): Promise<string> {
  const { stdout, stderr } = await execFileAsync(
    "docker",
    ["compose", "-p", project, "-f", "docker-compose.e2e.yaml", "logs", "--no-color", service],
    { cwd: repoRoot, maxBuffer: 128 * 1024 * 1024 },
  );
  return `${stdout}\n${stderr}`;
}

// parseOidcBearerDenialOutcomes extracts the distinct set of `outcome` values
// from the mcp-server JSON log's "oidc bearer token denied" lines
// (go/internal/oidcbearer/resolver.go's deny() emits the message plus
// iss + outcome only, never the raw token). The unified telemetry logger
// renames slog's "msg" key to "message" (go/internal/telemetry/logging.go's
// unifiedReplaceAttr), so the JSON carries "message", not "msg". This is the
// observable the distinct-denial matrix asserts on, because denial-side
// governance_audit reason_codes do not exist yet (tracked follow-up:
// eshu-hq/eshu#5567).
export function parseOidcBearerDenialOutcomes(logs: string): Set<string> {
  const outcomes = new Set<string>();
  for (const line of logs.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed.includes("oidc bearer token denied")) {
      continue;
    }
    // The compose log prefixes each line with "<service>  | "; find the JSON.
    const braceIdx = trimmed.indexOf("{");
    if (braceIdx === -1) {
      continue;
    }
    try {
      const parsed = JSON.parse(trimmed.slice(braceIdx)) as { message?: string; msg?: string; outcome?: string };
      const msg = parsed.message ?? parsed.msg;
      if (msg === "oidc bearer token denied" && typeof parsed.outcome === "string") {
        outcomes.add(parsed.outcome);
      }
    } catch {
      // Not a JSON denial line (e.g. a wrapped/continuation line); skip.
    }
  }
  return outcomes;
}
