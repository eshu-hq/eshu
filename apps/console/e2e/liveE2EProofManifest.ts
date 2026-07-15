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
  readonly corpus: {
    readonly identity: string;
    readonly repositoryCount: number;
  };
}

type ProofEnvironment = Readonly<Record<string, string | undefined>>;

function requiredValue(environment: ProofEnvironment, name: string): string {
  const value = environment[name]?.trim() ?? "";
  if (value === "") throw new Error(`${name} is required for durable live proof`);
  return value;
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
): LiveE2EProofManifest {
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
    corpus: {
      identity: requiredValue(environment, "ESHU_E2E_CORPUS_IDENTITY"),
      repositoryCount: requiredNonNegativeInteger(
        environment,
        "ESHU_E2E_CORPUS_REPOSITORY_COUNT",
      ),
    },
  };
}

// assertProofManifestRepositoryCount prevents a durable report from claiming a
// corpus identity that disagrees with the same-run authoritative API inventory.
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
