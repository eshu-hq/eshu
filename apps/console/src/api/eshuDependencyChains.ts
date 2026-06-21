// api/eshuDependencyChains.ts
// Bounded repo-scoped package dependency chain loader for
// GET /api/v0/package-registry/dependency-chains. The endpoint resolves
// consumer-repo -> package -> publisher-repo chains entirely on the read side by
// joining canonical manifest-backed consumption correlations with
// provenance-only publication/ownership correlations. The publisher leg is an
// inferred, provenance-only link — never an asserted repository dependency edge —
// so the view model keeps the consumption (canonical) leg and each publisher
// (inferred) leg structurally distinct. Live-API data only — no fabricated rows.

import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";
import type { EshuTruth } from "./envelope";

interface DependencyChainPublisherRecord {
  readonly correlation_id?: string;
  readonly relationship_kind?: string;
  readonly repository_id?: string;
  readonly repository_name?: string;
  readonly source_url?: string;
  readonly outcome?: string;
  readonly reason?: string;
  readonly provenance_only?: boolean;
  readonly canonical_writes?: number;
}

interface DependencyChainRecord {
  readonly consumer_repository_id?: string;
  readonly consumer_repository_name?: string;
  readonly package_id?: string;
  readonly package_name?: string;
  readonly ecosystem?: string;
  readonly dependency_range?: string;
  readonly consumption_correlation_id?: string;
  readonly consumption_provenance_only?: boolean;
  readonly consumption_canonical_writes?: number;
  readonly ambiguous?: boolean;
  readonly publishers?: readonly DependencyChainPublisherRecord[];
}

interface DependencyChainsResponse {
  readonly chains?: readonly DependencyChainRecord[];
  readonly repository_id?: string;
  readonly count?: number;
  readonly truncated?: boolean;
  readonly next_cursor?: { readonly after_correlation_id?: string };
}

// DependencyChainPublisher is one inferred, provenance-only publisher leg.
export interface DependencyChainPublisher {
  readonly correlationId: string;
  readonly relationshipKind: string;
  readonly repositoryId: string;
  readonly repositoryName: string;
  readonly sourceUrl: string;
  readonly outcome: string;
  readonly reason: string;
  // provenanceOnly is true for these legs today; surfaced so the UI renders an
  // inferred chip rather than an exact one.
  readonly provenanceOnly: boolean;
  readonly canonicalWrites: number;
}

// DependencyChain is one consumer-repo -> package -> publisher-repo chain.
export interface DependencyChain {
  readonly consumerRepositoryId: string;
  readonly consumerRepositoryName: string;
  readonly packageId: string;
  readonly packageName: string;
  readonly ecosystem: string;
  readonly dependencyRange: string;
  readonly consumptionCorrelationId: string;
  // consumptionProvenanceOnly is false for canonical manifest-backed
  // consumption; the consumption leg is the canonical part of the chain.
  readonly consumptionProvenanceOnly: boolean;
  readonly consumptionCanonicalWrites: number;
  // ambiguous is true when more than one candidate publisher repo remains; the
  // server never collapses that to a single asserted publisher.
  readonly ambiguous: boolean;
  readonly publishers: readonly DependencyChainPublisher[];
}

// DependencyChainPage is a bounded page plus paging and truth metadata.
export interface DependencyChainPage {
  readonly chains: readonly DependencyChain[];
  readonly repositoryId: string;
  readonly truncated: boolean;
  readonly truth: EshuTruth | null;
}

function publisherFromRecord(record: DependencyChainPublisherRecord): DependencyChainPublisher {
  return {
    correlationId: record.correlation_id ?? "",
    relationshipKind: record.relationship_kind ?? "",
    repositoryId: record.repository_id ?? "",
    repositoryName: record.repository_name ?? "",
    sourceUrl: record.source_url ?? "",
    outcome: record.outcome ?? "",
    reason: record.reason ?? "",
    provenanceOnly: record.provenance_only !== false,
    canonicalWrites: record.canonical_writes ?? 0
  };
}

function chainFromRecord(record: DependencyChainRecord): DependencyChain {
  return {
    consumerRepositoryId: record.consumer_repository_id ?? "",
    consumerRepositoryName: record.consumer_repository_name ?? "",
    packageId: record.package_id ?? "",
    packageName: record.package_name ?? "",
    ecosystem: record.ecosystem ?? "",
    dependencyRange: record.dependency_range ?? "",
    consumptionCorrelationId: record.consumption_correlation_id ?? "",
    consumptionProvenanceOnly: Boolean(record.consumption_provenance_only),
    consumptionCanonicalWrites: record.consumption_canonical_writes ?? 0,
    ambiguous: Boolean(record.ambiguous),
    publishers: (record.publishers ?? []).map(publisherFromRecord)
  };
}

// loadDependencyChains runs one bounded repo-scoped chain lookup and returns a
// typed page with truth metadata. The repository selector may be a canonical id
// or a human selector (name, slug, path, or remote URL).
export async function loadDependencyChains(
  client: EshuApiClient,
  repository: string,
  limit = 50
): Promise<DependencyChainPage> {
  const params = new URLSearchParams();
  params.set("repository_id", repository);
  params.set("limit", String(limit));
  const env = await client.get<DependencyChainsResponse>(
    `/api/v0/package-registry/dependency-chains?${params.toString()}`
  );
  if (env.error) throw new EshuEnvelopeError(env.error);
  const records = env.data?.chains ?? [];
  return {
    chains: records.map(chainFromRecord),
    repositoryId: env.data?.repository_id ?? repository,
    truncated: Boolean(env.data?.truncated),
    truth: env.truth
  };
}
