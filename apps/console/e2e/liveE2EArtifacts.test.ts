import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import {
  liveE2EArtifactPaths,
  prepareLiveE2EArtifacts,
} from "./liveE2EArtifacts.ts";

describe("console live E2E artifact ownership", () => {
  it("preserves the existing durable proof-id character contract", () => {
    const paths = liveE2EArtifactPaths("/repo", "dashboard-proof-2026.07.15");

    expect(paths.rootDir).toBe(
      "/repo/e2e-artifacts/console-live-e2e/dashboard-proof-2026.07.15",
    );
  });

  it("isolates cleanup and outputs between concurrent proof identities", async () => {
    const repoRoot = await mkdtemp(join(tmpdir(), "eshu-live-e2e-artifacts-"));
    const first = liveE2EArtifactPaths(repoRoot, "proof_first");
    const second = liveE2EArtifactPaths(repoRoot, "proof_second");

    await prepareLiveE2EArtifacts(first);
    await prepareLiveE2EArtifacts(second);
    await writeFile(first.reportPath, "first", "utf8");
    await writeFile(second.reportPath, "second", "utf8");

    await prepareLiveE2EArtifacts(first);

    await expect(readFile(second.reportPath, "utf8")).resolves.toBe("second");
    await expect(readFile(first.reportPath, "utf8")).rejects.toMatchObject({ code: "ENOENT" });
    expect(first.rootDir).not.toBe(second.rootDir);
  });

  it.each(["", "../other-proof", "proof/other", "proof other"])(
    "rejects unsafe proof identity %j",
    (proofId) => {
      expect(() => liveE2EArtifactPaths("/repo", proofId)).toThrow(
        "ESHU_E2E_PROOF_ID must start with an alphanumeric character",
      );
    },
  );
});
