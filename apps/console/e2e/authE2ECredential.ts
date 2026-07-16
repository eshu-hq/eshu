// authE2ECredential.ts — host-side wrapper around `eshu admin
// initial-credential` for the browser-auth E2E runner (issue #4971 phase 2).
//
// The generated one-time bootstrap admin credential lives sealed in Postgres
// (see go/cmd/eshu/admin_initial_credential.go); the only supported way to
// read it is the `eshu` CLI itself, run directly against the same Postgres
// DSN and data-encryption key the `eshu` API container uses. This module
// shells out to `go run ./cmd/eshu admin initial-credential` from the go/
// module root (mirrors scripts/verify-pagerduty-marketplace-readiness.sh's
// `go -C "$repo_root/go" run ./cmd/eshu "$@"` pattern) rather than requiring
// a prebuilt `eshu` binary on PATH, so the gate has no separate build step.
import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

// e2eDefaultAuthSecretEncKey is the fixed, publicly-known, all-zero dev-only
// data-encryption key docker-compose.yaml ships as ESHU_AUTH_SECRET_ENC_KEY's
// default (see that file's comment). docker-compose.e2e.yaml does not
// override this variable for the `eshu` service, so the e2e stack's
// container uses the same default unless the environment overrides it — this
// constant mirrors that default so the host-side CLI call can open the same
// sealed envelope without requiring the operator to export anything extra.
const e2eDefaultAuthSecretEncKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";

export interface AuthE2EPostgresConfig {
  readonly host: string;
  readonly port: string;
  readonly password: string;
  readonly database: string;
}

// buildPostgresDSN constructs the host-reachable Postgres DSN for the e2e
// stack's isolated `postgres` service (docker-compose.e2e.yaml maps it to
// ESHU_E2E_POSTGRES_PORT on 127.0.0.1). sslmode=disable matches the other
// host-side local-Postgres scripts in this repo (no TLS on the local Compose
// network).
export function buildPostgresDSN(cfg: AuthE2EPostgresConfig): string {
  return `postgresql://eshu:${cfg.password}@${cfg.host}:${cfg.port}/${cfg.database}?sslmode=disable`;
}

export interface InitialCredential {
  readonly username: string;
  readonly password: string;
  readonly recoveryCode: string;
}

export interface RetrieveCredentialResult {
  readonly status: "available" | "unavailable" | "error";
  readonly credential: InitialCredential | null;
  readonly failureReason: CredentialFailureReason | null;
  // rawStderr is captured for diagnostics only — never printed alongside a
  // successful credential (the plaintext password must not be logged twice).
  readonly rawStderr: string;
}

export type CredentialFailureReason =
  | "credential_unavailable"
  | "encryption_key_mismatch"
  | "credential_command_timeout"
  | "postgres_unavailable"
  | "credential_command_failed";

export function classifyCredentialFailure(diagnostic: string): {
  readonly status: "unavailable" | "error";
  readonly reason: CredentialFailureReason;
} {
  const normalized = diagnostic.toLowerCase();
  if (normalized.includes("no retrievable bootstrap credential")) {
    return { status: "unavailable", reason: "credential_unavailable" };
  }
  if (normalized.includes("cannot decrypt the sealed bootstrap credential")) {
    return { status: "error", reason: "encryption_key_mismatch" };
  }
  if (normalized.includes("timed out") || normalized.includes("timeout")) {
    return { status: "error", reason: "credential_command_timeout" };
  }
  if (
    normalized.includes("postgres") ||
    normalized.includes("connection refused") ||
    normalized.includes("select bootstrap credential")
  ) {
    return { status: "error", reason: "postgres_unavailable" };
  }
  return { status: "error", reason: "credential_command_failed" };
}

const usernameLine = /^username:\s+(\S+)/m;
const passwordLine = /^password:\s+(\S+)/m;
const recoveryLine = /^recovery code:\s+(\S+)/m;

// retrieveInitialCredential runs `eshu admin initial-credential` against the
// e2e stack's Postgres and returns the parsed one-time credential, or `null`
// once it has already been consumed (claimed) or was never generated — both
// of which exit non-zero with an explanatory stderr message
// (go/cmd/eshu/admin_initial_credential.go's openBootstrapCredentialPayload).
// A `null` return is an expected outcome, not a runner failure: callers use
// it both to fetch the credential the first time and to prove consumption
// after the setup wizard completes (acceptance item 2).
export async function retrieveInitialCredential(
  repoGoDir: string,
  postgresDSN: string,
  authSecretEncKey: string,
): Promise<RetrieveCredentialResult> {
  try {
    const { stdout } = await execFileAsync(
      "go",
      ["-C", repoGoDir, "run", "./cmd/eshu", "admin", "initial-credential"],
      {
        env: {
          ...process.env,
          ESHU_POSTGRES_DSN: postgresDSN,
          ESHU_AUTH_SECRET_ENC_KEY: authSecretEncKey,
        },
        timeout: 60000,
      },
    );
    const username = usernameLine.exec(stdout)?.[1];
    const password = passwordLine.exec(stdout)?.[1];
    const recoveryCode = recoveryLine.exec(stdout)?.[1];
    if (!username || !password || !recoveryCode) {
      // Report only WHICH fields were missing, never the raw stdout: on a fresh
      // stack that stdout carries the one-time bootstrap password and recovery
      // code, and this message flows into rawStderr which item 2 prints to the
      // E2E log/report — embedding stdout would turn a CLI wording change into a
      // real credential leak (the exact thing item 6 exists to prevent).
      throw new Error(
        `unable to parse credential fields from \`eshu admin initial-credential\` stdout ` +
          `(username=${Boolean(username)}, password=${Boolean(password)}, recoveryCode=${Boolean(recoveryCode)}); ` +
          `stdout omitted because it carries the one-time credential`,
      );
    }
    return {
      status: "available",
      credential: { username, password, recoveryCode },
      failureReason: null,
      rawStderr: "",
    };
  } catch (err) {
    const stderr =
      err !== null && typeof err === "object" && "stderr" in err
        ? String((err as { stderr: unknown }).stderr)
        : err instanceof Error
          ? err.message
          : String(err);
    const failure = classifyCredentialFailure(stderr);
    return {
      status: failure.status,
      credential: null,
      failureReason: failure.reason,
      rawStderr: stderr,
    };
  }
}

export { e2eDefaultAuthSecretEncKey };
