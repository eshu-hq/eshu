export interface LiveE2EProofManifest {
  readonly proofId: string;
  readonly sourceHash: string;
  readonly runnerHash: string;
  readonly api: {
    readonly imageDigest: string;
    readonly version: string;
  };
  readonly nornic: {
    readonly imageDigest: string;
    readonly version: string;
  };
  readonly runtime: {
    readonly nodeVersion: string;
    readonly playwrightVersion: string;
    readonly launchedBrowserVersion: string;
  };
  readonly corpus: {
    readonly operatorAttestation: string;
    readonly repositoryCount: number;
    readonly validation: {
      readonly operatorAttestation: "operator_attested_not_authoritatively_validated";
      readonly repositoryCount: "validated_against_live_inventory";
    };
  };
}

type ProofEnvironment = Readonly<Record<string, string | undefined>>;

function requiredValue(environment: ProofEnvironment, name: string): string {
  const value = environment[name]?.trim() ?? "";
  if (value === "") throw new Error(`${name} is required for durable live proof`);
  return value;
}

function requiredCorpusAttestation(environment: ProofEnvironment): string {
  const attestation = environment.ESHU_E2E_CORPUS_ATTESTATION?.trim() ?? "";
  if (attestation !== "") return attestation;
  const deprecatedIdentity = environment.ESHU_E2E_CORPUS_IDENTITY?.trim() ?? "";
  if (deprecatedIdentity !== "") return deprecatedIdentity;
  throw new Error(
    "ESHU_E2E_CORPUS_ATTESTATION is required for durable live proof " +
      "(ESHU_E2E_CORPUS_IDENTITY is accepted as a deprecated fallback)",
  );
}

function requiredSHA256(environment: ProofEnvironment, name: string): string {
  const value = requiredValue(environment, name).toLowerCase();
  if (!/^[a-f0-9]{64}$/.test(value)) {
    throw new Error(`${name} must be a 64-character SHA-256 hash`);
  }
  return value;
}

function requiredImageDigest(environment: ProofEnvironment, name: string): string {
  const value = requiredValue(environment, name).toLowerCase();
  if (!/^sha256:[a-f0-9]{64}$/.test(value)) {
    throw new Error(`${name} must be an immutable sha256 image digest`);
  }
  return value;
}

function requiredNonNegativeInteger(environment: ProofEnvironment, name: string): number {
  const value = requiredValue(environment, name);
  if (!/^\d+$/.test(value)) throw new Error(`${name} must be a non-negative integer`);
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed)) throw new Error(`${name} exceeds the safe integer range`);
  return parsed;
}

// proofManifestFromEnvironment builds the non-secret identity block persisted
// with every durable browser report. Credentials are deliberately not accepted
// into this shape, so serializing the manifest cannot expose them.
export function proofManifestFromEnvironment(
  environment: ProofEnvironment,
  launchedBrowserVersion: string,
): LiveE2EProofManifest {
  const browserVersion = launchedBrowserVersion.trim();
  if (browserVersion === "") throw new Error("launched browser version is required");
  return {
    proofId: requiredValue(environment, "ESHU_E2E_PROOF_ID"),
    sourceHash: requiredSHA256(environment, "ESHU_E2E_SOURCE_HASH"),
    runnerHash: requiredSHA256(environment, "ESHU_E2E_RUNNER_HASH"),
    api: {
      imageDigest: requiredImageDigest(environment, "ESHU_E2E_API_IMAGE_DIGEST"),
      version: requiredValue(environment, "ESHU_E2E_API_VERSION"),
    },
    nornic: {
      imageDigest: requiredImageDigest(environment, "ESHU_E2E_NORNIC_IMAGE_DIGEST"),
      version: requiredValue(environment, "ESHU_E2E_NORNIC_VERSION"),
    },
    runtime: {
      nodeVersion: requiredValue(environment, "ESHU_E2E_NODE_VERSION"),
      playwrightVersion: requiredValue(environment, "ESHU_E2E_PLAYWRIGHT_VERSION"),
      launchedBrowserVersion: browserVersion,
    },
    corpus: {
      operatorAttestation: requiredCorpusAttestation(environment),
      repositoryCount: requiredNonNegativeInteger(environment, "ESHU_E2E_CORPUS_REPOSITORY_COUNT"),
      validation: {
        operatorAttestation: "operator_attested_not_authoritatively_validated",
        repositoryCount: "validated_against_live_inventory",
      },
    },
  };
}

// assertProofManifestRepositoryCount validates only the repository cardinality
// against the same-run API inventory. The operator attestation is deliberately
// reported as non-authoritative rather than pretending the runner can derive a
// canonical retained-corpus identity from that single count.
export function assertProofManifestRepositoryCount(
  manifest: LiveE2EProofManifest,
  liveInventoryTotal: number,
): void {
  if (manifest.corpus.repositoryCount !== liveInventoryTotal) {
    throw new Error(
      `declared corpus repository count ${manifest.corpus.repositoryCount} did not match live inventory total ${liveInventoryTotal}`,
    );
  }
}
