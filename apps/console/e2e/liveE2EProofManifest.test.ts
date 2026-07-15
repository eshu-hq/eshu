import { describe, expect, it } from "vitest";

import {
  assertProofManifestRepositoryCount,
  proofManifestFromEnvironment,
} from "./liveE2EProofManifest";

const validEnvironment = {
  ESHU_E2E_PROOF_ID: "dashboard-proof-20260715",
  ESHU_E2E_SOURCE_HASH: "a".repeat(64),
  ESHU_E2E_RUNNER_HASH: "b".repeat(64),
  ESHU_E2E_API_IMAGE_DIGEST: `sha256:${"c".repeat(64)}`,
  ESHU_E2E_API_VERSION: "proof-a1b2c3",
  ESHU_E2E_NORNIC_IMAGE_DIGEST: `sha256:${"d".repeat(64)}`,
  ESHU_E2E_NORNIC_VERSION: "v1.1.11",
  ESHU_E2E_CORPUS_IDENTITY: "retained-task-777",
  ESHU_E2E_CORPUS_REPOSITORY_COUNT: "896",
  ESHU_E2E_API_KEY: "must-never-appear",
  ESHU_AUTH_SECRET_ENC_KEY: "must-never-appear-either",
} as const;

describe("live E2E proof manifest", () => {
  it("binds the report to non-secret immutable runtime and corpus identity", () => {
    const manifest = proofManifestFromEnvironment(validEnvironment);

    expect(manifest).toEqual({
      proofId: "dashboard-proof-20260715",
      sourceHash: "a".repeat(64),
      runnerHash: "b".repeat(64),
      api: {
        imageDigest: `sha256:${"c".repeat(64)}`,
        version: "proof-a1b2c3",
      },
      nornic: {
        imageDigest: `sha256:${"d".repeat(64)}`,
        version: "v1.1.11",
      },
      corpus: {
        identity: "retained-task-777",
        repositoryCount: 896,
      },
    });
    expect(JSON.stringify(manifest)).not.toContain("must-never-appear");
  });

  it("fails closed when an immutable identity is missing or malformed", () => {
    expect(() =>
      proofManifestFromEnvironment({ ...validEnvironment, ESHU_E2E_RUNNER_HASH: "" }),
    ).toThrow("ESHU_E2E_RUNNER_HASH");
    expect(() =>
      proofManifestFromEnvironment({
        ...validEnvironment,
        ESHU_E2E_CORPUS_REPOSITORY_COUNT: "not-a-count",
      }),
    ).toThrow("ESHU_E2E_CORPUS_REPOSITORY_COUNT");
  });

  it("fails closed when the declared corpus count differs from the live inventory", () => {
    const manifest = proofManifestFromEnvironment(validEnvironment);

    expect(() => assertProofManifestRepositoryCount(manifest, 887)).toThrow(
      "declared corpus repository count 896 did not match live inventory total 887",
    );
    expect(() => assertProofManifestRepositoryCount(manifest, 896)).not.toThrow();
  });
});
