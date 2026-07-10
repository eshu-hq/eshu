// Item-6 negative-leakage scan (issue #4971): prove that the setup/SSO flow's
// sensitive material — the generated bootstrap credential, MFA recovery codes,
// and provider client secrets — never reaches an operator-visible surface
// (container logs, audit payloads, status/health endpoints, admin API
// responses, or the rendered DOM).
//
// The pure functions below carry the detection and self-integrity logic and are
// unit-tested in authE2ELeakage.test.ts. The surface collectors that need a
// live stack (container logs) live here too but are exercised by the full
// runAuthE2E gate rather than the unit test.
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import type { Page } from "playwright";

const execFileAsync = promisify(execFile);

// SecretProbe pairs a human-readable label with the exact plaintext secret to
// search for. The label is what a failure names; the value is never logged.
export interface SecretProbe {
  readonly label: string;
  readonly value: string;
}

// LeakageSurface is one operator-visible text blob to scan (its human name plus
// the full body).
export interface LeakageSurface {
  readonly name: string;
  readonly body: string;
}

// findLeakedProbes returns the labels of every probe whose value occurs in the
// haystack. A blank (empty or whitespace-only) probe value is skipped rather
// than matched: String.prototype.includes("") is always true, so an empty
// needle would otherwise "find" itself in every surface and turn the scan into
// a vacuous pass. assertProbesNonEmpty is the companion guard that rejects such
// probes up front; this skip is the second line of defence.
export function findLeakedProbes(haystack: string, probes: readonly SecretProbe[]): string[] {
  return probes
    .filter((probe) => probe.value.trim().length > 0 && haystack.includes(probe.value))
    .map((probe) => probe.label);
}

// stripBootstrapBanner removes the one-time ESHU bootstrap-admin credential
// banner from a container log. That banner (seed_initial_admin_helpers.go's
// printBootstrapBanner) is epic #4962's OWN sanctioned plaintext surface — the
// one-time startup print an operator reads to grab the generated credential,
// alongside the `eshu admin initial-credential` CLI and the Helm secret. The
// negative-leakage invariant is that the credential appears NOWHERE ELSE, so
// item 6 scans the log with this banner block removed. It fails closed: if the
// start marker is present but the terminating line is not (a truncated or
// malformed banner), it returns the log UNCHANGED so a real leak riding along a
// broken banner is never silently masked.
const bootstrapBannerStart = "ESHU BOOTSTRAP ADMIN CREDENTIAL";
const bootstrapBannerEnd = "Save these values now";

export function stripBootstrapBanner(logs: string): string {
  const lines = logs.split("\n");
  const start = lines.findIndex((line) => line.includes(bootstrapBannerStart));
  if (start === -1) {
    return logs;
  }
  const end = lines.findIndex((line, index) => index > start && line.includes(bootstrapBannerEnd));
  if (end === -1) {
    return logs;
  }
  lines.splice(start, end - start + 1);
  return lines.join("\n");
}

// assertProbesNonEmpty guards the scan's own integrity: if the harness failed
// to capture a secret it means to prove absent, scanning for "" would pass
// vacuously. Every probe MUST carry a real value, so a mis-wired capture fails
// loudly instead of silently green.
export function assertProbesNonEmpty(probes: readonly SecretProbe[]): void {
  const blank = probes
    .filter((probe) => probe.value.trim().length === 0)
    .map((probe) => probe.label);
  if (blank.length > 0) {
    throw new Error(`leakage scan misconfigured: empty secret probe(s): ${blank.join(", ")}`);
  }
}

// assertAdminReadsSucceeded fails closed if any admin-session API read this
// scan collected did not return 200: a non-200 (e.g. 401 from a revoked
// session — issue #5002 P2, codex PR #5053 review) means the read never
// actually reached the surface it claims to scan, silently hollowing out the
// leak check for that surface rather than proving it clean.
export function assertAdminReadsSucceeded(
  reads: readonly { readonly name: string; readonly status: number }[],
): void {
  const failed = reads.filter((read) => read.status !== 200);
  if (failed.length > 0) {
    throw new Error(
      `leakage scan admin read(s) did not return 200 (session may be unauthenticated): ${failed
        .map((read) => `${read.name}=${read.status}`)
        .join(", ")}`,
    );
  }
}

// scanSurfacesForLeakage checks every surface against every probe and returns a
// "<secret> leaked into <surface>" finding for each match, in surface-then-probe
// order. An empty result means no secret reached any scanned surface.
export function scanSurfacesForLeakage(
  surfaces: readonly LeakageSurface[],
  probes: readonly SecretProbe[],
): string[] {
  const findings: string[] = [];
  for (const surface of surfaces) {
    for (const label of findLeakedProbes(surface.body, probes)) {
      findings.push(`${label} leaked into ${surface.name}`);
    }
  }
  return findings;
}

// collectApiContainerLogs returns the eshu API container's full stdout+stderr
// from the compose stack so the scan can prove no secret was logged. It reads
// the whole log (--no-color, no --tail) with a generous buffer; a non-empty
// return is required by the caller so an empty read never passes vacuously.
export async function collectApiContainerLogs(repoRoot: string, project: string): Promise<string> {
  const { stdout, stderr } = await execFileAsync(
    "docker",
    ["compose", "-p", project, "-f", "docker-compose.e2e.yaml", "logs", "--no-color", "eshu"],
    { cwd: repoRoot, maxBuffer: 128 * 1024 * 1024 },
  );
  return `${stdout}\n${stderr}`;
}

// apiFetchFn is the injected "fetch through a real browser session" helper
// (authE2EOidcFlow.ts's apiFetchInPage) — passed in rather than imported to
// keep this module free of an import cycle and unit-testable in isolation.
type apiFetchFn = (
  page: Page,
  method: string,
  path: string,
  body?: Record<string, unknown>,
) => Promise<{ status: number; text: string }>;

// LeakageScanInputs are the live surfaces item 6 draws from: the admin browser
// session (audit/provider-config reads + admin DOM), the member session's DOM,
// the API base for unauthenticated status/health probes, and the compose
// coordinates for the container-log read.
export interface LeakageScanInputs {
  readonly adminPage: Page;
  readonly memberPage: Page;
  readonly apiFetchInPage: apiFetchFn;
  readonly apiBase: string;
  readonly repoRoot: string;
  readonly project: string;
  readonly probes: readonly SecretProbe[];
}

// runLeakageScan gathers every operator-visible surface the setup/SSO flow
// touched and proves no secret probe appears in any of them. It fails closed
// three ways: (1) assertProbesNonEmpty rejects a mis-captured (blank) secret so
// the scan can never pass vacuously; (2) the container-log and DOM surfaces —
// the ones whose emptiness would mean "we never actually read anything" — are
// asserted non-empty; (3) any probe found in any surface throws with the
// offending secret+surface named. On success it returns a one-line summary.
export async function runLeakageScan(inputs: LeakageScanInputs): Promise<string> {
  assertProbesNonEmpty(inputs.probes);

  const surfaces: LeakageSurface[] = [];

  // Admin-session API reads: audit trail (events + summary), provider-config
  // list. None of these should ever echo a secret back to an admin caller.
  const adminReads: { name: string; status: number }[] = [];
  for (const [name, path] of [
    ["audit events API", "/api/v0/auth/admin/audit/events"],
    ["audit summary API", "/api/v0/auth/admin/audit/summary"],
    ["provider-configs API", "/api/v0/auth/admin/provider-configs"],
  ] as const) {
    const res = await inputs.apiFetchInPage(inputs.adminPage, "GET", path);
    adminReads.push({ name, status: res.status });
    surfaces.push({ name, body: res.text });
  }
  // Fail closed BEFORE scanning bodies: a non-200 admin read (e.g. 401 from a
  // revoked session) never actually reached the surface it claims to cover,
  // so scanning its (error) body for secrets would silently prove nothing.
  assertAdminReadsSucceeded(adminReads);

  // Unauthenticated status/health endpoints.
  for (const path of ["/healthz", "/readyz"] as const) {
    const res = await fetch(`${inputs.apiBase}${path}`);
    surfaces.push({ name: `${path} endpoint`, body: await res.text() });
  }

  // Rendered DOM of both the admin and the member dashboards.
  const adminDom = await inputs.adminPage.content();
  const memberDom = await inputs.memberPage.content();
  surfaces.push({ name: "admin DOM", body: adminDom });
  surfaces.push({ name: "member DOM", body: memberDom });

  // Full API container log, minus the one-time bootstrap-admin banner — epic
  // #4962's sanctioned plaintext surface (seed_initial_admin_helpers.go). The
  // banner MUST be present (the first-run credential mechanism is intact); the
  // credential appearing anywhere OUTSIDE it is a real leak, so the log is
  // scanned with the banner block removed.
  const rawLogs = await collectApiContainerLogs(inputs.repoRoot, inputs.project);
  if (!rawLogs.includes("ESHU BOOTSTRAP ADMIN CREDENTIAL")) {
    throw new Error(
      "API container log has no one-time bootstrap banner; the first-run credential surface changed unexpectedly",
    );
  }
  const scannedLogsName = "API container logs (excluding one-time bootstrap banner)";
  surfaces.push({ name: scannedLogsName, body: stripBootstrapBanner(rawLogs) });

  // Integrity: the log and DOM surfaces MUST have content — an empty read there
  // means the scan looked at nothing and would pass for the wrong reason.
  const criticalEmpty = surfaces
    .filter(
      (surface) =>
        [scannedLogsName, "admin DOM", "member DOM"].includes(surface.name) &&
        surface.body.trim().length === 0,
    )
    .map((surface) => surface.name);
  if (criticalEmpty.length > 0) {
    throw new Error(
      `leakage scan read empty critical surface(s), proving nothing: ${criticalEmpty.join(", ")}`,
    );
  }

  const findings = scanSurfacesForLeakage(surfaces, inputs.probes);
  if (findings.length > 0) {
    throw new Error(`secret leakage detected: ${findings.join("; ")}`);
  }
  return `no secret leaked across ${surfaces.length} surfaces (audit, provider-configs, health, DOM, container logs); ${inputs.probes.length} secrets checked`;
}
