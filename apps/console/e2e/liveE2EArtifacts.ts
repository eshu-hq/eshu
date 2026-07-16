import { mkdir, rm } from "node:fs/promises";
import { resolve } from "node:path";

export interface LiveE2EArtifactPaths {
  readonly rootDir: string;
  readonly screenshotsDir: string;
  readonly tracePath: string;
  readonly reportPath: string;
}

const safeProofId = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/;

// liveE2EArtifactPaths gives one proof identity exclusive ownership of its
// outputs. The retained harness accepts a stricter subset for its database
// schema and sidecar names; both contracts remain safe path segments.
export function liveE2EArtifactPaths(
  repoRoot: string,
  proofId: string,
): LiveE2EArtifactPaths {
  if (!safeProofId.test(proofId)) {
    throw new Error(
      "ESHU_E2E_PROOF_ID must start with an alphanumeric character and contain only alphanumerics, dot, underscore, or hyphen (maximum 128 characters)",
    );
  }
  const rootDir = resolve(repoRoot, "e2e-artifacts", "console-live-e2e", proofId);
  return {
    rootDir,
    screenshotsDir: resolve(rootDir, "screenshots"),
    tracePath: resolve(rootDir, "trace.zip"),
    reportPath: resolve(rootDir, "console-live-e2e-report.json"),
  };
}

// prepareLiveE2EArtifacts removes only the calling proof's output tree.
export async function prepareLiveE2EArtifacts(paths: LiveE2EArtifactPaths): Promise<void> {
  await rm(paths.rootDir, { recursive: true, force: true });
  await mkdir(paths.screenshotsDir, { recursive: true });
}
