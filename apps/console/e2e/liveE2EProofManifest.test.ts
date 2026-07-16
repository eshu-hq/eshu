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
  ESHU_E2E_NODE_VERSION: "v24.4.1",
  ESHU_E2E_PLAYWRIGHT_VERSION: "1.60.0",
  ESHU_E2E_CORPUS_ATTESTATION: "retained-task-777",
  ESHU_E2E_CORPUS_REPOSITORY_COUNT: "896",
  ESHU_E2E_API_KEY: "must-never-appear",
  ESHU_AUTH_SECRET_ENC_KEY: "must-never-appear-either",
} as const;

describe("live E2E proof manifest", () => {
  it("binds the report to immutable runtime inputs and an explicit corpus attestation", () => {
    const manifest = proofManifestFromEnvironment(validEnvironment, "Chromium 140.0.7339.16");

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
      runtime: {
        nodeVersion: "v24.4.1",
        playwrightVersion: "1.60.0",
        launchedBrowserVersion: "Chromium 140.0.7339.16",
      },
      corpus: {
        operatorAttestation: "retained-task-777",
        repositoryCount: 896,
        validation: {
          operatorAttestation: "operator_attested_not_authoritatively_validated",
          repositoryCount: "validated_against_live_inventory",
        },
      },
    });
    expect(JSON.stringify(manifest)).not.toContain("must-never-appear");
  });

  it("fails closed when an immutable identity is missing or malformed", () => {
    expect(() =>
      proofManifestFromEnvironment(
        { ...validEnvironment, ESHU_E2E_RUNNER_HASH: "" },
        "Chromium 140.0.7339.16",
      ),
    ).toThrow("ESHU_E2E_RUNNER_HASH");
    expect(() =>
      proofManifestFromEnvironment(
        {
          ...validEnvironment,
          ESHU_E2E_CORPUS_REPOSITORY_COUNT: "not-a-count",
        },
        "Chromium 140.0.7339.16",
      ),
    ).toThrow("ESHU_E2E_CORPUS_REPOSITORY_COUNT");
    expect(() => proofManifestFromEnvironment(validEnvironment, "")).toThrow(
      "launched browser version",
    );
  });

  it("fails closed when the declared corpus count differs from the live inventory", () => {
    const manifest = proofManifestFromEnvironment(validEnvironment, "Chromium 140.0.7339.16");

    expect(() => assertProofManifestRepositoryCount(manifest, 887)).toThrow(
      "declared corpus repository count 896 did not match live inventory total 887",
    );
    expect(() => assertProofManifestRepositoryCount(manifest, 896)).not.toThrow();
  });

  it("reports the operator corpus attestation without claiming to validate it", () => {
    const manifest = proofManifestFromEnvironment(
      {
        ...validEnvironment,
        ESHU_E2E_CORPUS_ATTESTATION: "operator-label-with-the-same-live-count",
      },
      "Chromium 140.0.7339.16",
    );

    expect(manifest.corpus.operatorAttestation).toBe("operator-label-with-the-same-live-count");
    expect(manifest.corpus.validation).toEqual({
      operatorAttestation: "operator_attested_not_authoritatively_validated",
      repositoryCount: "validated_against_live_inventory",
    });
    expect(() => assertProofManifestRepositoryCount(manifest, 896)).not.toThrow();
  });

  it("prefers the attestation variable and accepts the deprecated identity fallback", () => {
    const preferred = proofManifestFromEnvironment(
      {
        ...validEnvironment,
        ESHU_E2E_CORPUS_ATTESTATION: "new-attestation",
        ESHU_E2E_CORPUS_IDENTITY: "deprecated-identity",
      },
      "Chromium 140.0.7339.16",
    );
    const fallbackEnvironment = {
      ...validEnvironment,
      ESHU_E2E_CORPUS_ATTESTATION: undefined,
      ESHU_E2E_CORPUS_IDENTITY: "deprecated-identity",
    };
    const fallback = proofManifestFromEnvironment(fallbackEnvironment, "Chromium 140.0.7339.16");

    expect(preferred.corpus.operatorAttestation).toBe("new-attestation");
    expect(fallback.corpus.operatorAttestation).toBe("deprecated-identity");
  });
});
