import type { EshuApiClient } from "./client";
import type { EshuTruth } from "./envelope";
import { EshuEnvelopeError, unwrapEnvelope } from "./envelope";

export interface InvestigationPacketResult {
  readonly packet: InvestigationEvidencePacket;
  readonly truth: EshuTruth;
}

export interface InvestigationEvidencePacket {
  readonly answer: Record<string, unknown>;
  readonly bounds: Record<string, unknown>;
  readonly graphAnswers: readonly Record<string, unknown>[];
  readonly identity: Record<string, unknown>;
  readonly missingEvidence: readonly Record<string, unknown>[];
  readonly packetId: string;
  readonly reducerDecisions: readonly Record<string, unknown>[];
  readonly redaction: Record<string, unknown>;
  readonly refusal: string;
  readonly reproduce: readonly Record<string, unknown>[];
  readonly schema: string;
  readonly semanticObservations: readonly Record<string, unknown>[];
  readonly sourceFacts: readonly Record<string, unknown>[];
  readonly validation: Record<string, unknown>;
}

export interface SupplyChainImpactPacketQuery {
  readonly advisoryId?: string;
  readonly cveId?: string;
  readonly findingId?: string;
  readonly imageRef?: string;
  readonly maxSourceFacts?: number;
  readonly packageId?: string;
  readonly repositoryId?: string;
  readonly serviceId?: string;
  readonly subjectDigest?: string;
  readonly workloadId?: string;
}

export interface DeployableUnitPacketQuery {
  readonly generationId: string;
  readonly maxSourceFacts?: number;
  readonly repositoryId?: string;
  readonly scopeId: string;
}

export interface CloudRuntimeDriftPacketQuery {
  readonly accountId?: string;
  readonly cloudResourceUid?: string;
  readonly maxSourceFacts?: number;
  readonly projectId?: string;
  readonly provider?: string;
  readonly scopeId?: string;
  readonly subscriptionId?: string;
}

interface InvestigationEvidencePacketWire {
  readonly answer?: Record<string, unknown>;
  readonly bounds?: Record<string, unknown>;
  readonly graph_answers?: readonly Record<string, unknown>[];
  readonly identity?: Record<string, unknown>;
  readonly missing_evidence?: readonly Record<string, unknown>[];
  readonly packet_id?: string;
  readonly reducer_decisions?: readonly Record<string, unknown>[];
  readonly redaction?: Record<string, unknown>;
  readonly refusal?: string;
  readonly reproduce?: readonly Record<string, unknown>[];
  readonly schema?: string;
  readonly semantic_observations?: readonly Record<string, unknown>[];
  readonly source_facts?: readonly Record<string, unknown>[];
  readonly validation?: Record<string, unknown>;
}

export async function loadSupplyChainImpactPacket(
  client: EshuApiClient,
  query: SupplyChainImpactPacketQuery
): Promise<InvestigationPacketResult> {
  return loadPacket(client, "/api/v0/investigations/supply-chain/impact/packet", {
    finding_id: query.findingId,
    advisory_id: query.advisoryId,
    cve_id: query.cveId,
    package_id: query.packageId,
    repository_id: query.repositoryId,
    subject_digest: query.subjectDigest,
    image_ref: query.imageRef,
    workload_id: query.workloadId,
    service_id: query.serviceId,
    max_source_facts: query.maxSourceFacts
  });
}

export async function loadDeployableUnitPacket(
  client: EshuApiClient,
  query: DeployableUnitPacketQuery
): Promise<InvestigationPacketResult> {
  return loadPacket(client, "/api/v0/investigations/deployable-unit/packet", {
    scope_id: query.scopeId,
    generation_id: query.generationId,
    repository_id: query.repositoryId,
    max_source_facts: query.maxSourceFacts
  });
}

export async function loadCloudRuntimeDriftPacket(
  client: EshuApiClient,
  query: CloudRuntimeDriftPacketQuery
): Promise<InvestigationPacketResult> {
  return loadPacket(client, "/api/v0/investigations/drift/packet", {
    scope_id: query.scopeId,
    account_id: query.accountId,
    project_id: query.projectId,
    subscription_id: query.subscriptionId,
    provider: query.provider,
    cloud_resource_uid: query.cloudResourceUid,
    max_source_facts: query.maxSourceFacts
  });
}

async function loadPacket(
  client: EshuApiClient,
  route: string,
  query: Record<string, string | number | undefined>
): Promise<InvestigationPacketResult> {
  const env = await client.get<InvestigationEvidencePacketWire>(routeWithQuery(route, query));
  if (env.error) throw new EshuEnvelopeError(env.error);
  const { data, truth } = unwrapEnvelope(env);
  return { packet: packetFromWire(data), truth };
}

function routeWithQuery(route: string, query: Record<string, string | number | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && String(value).trim().length > 0) {
      params.set(key, String(value));
    }
  }
  const encoded = params.toString();
  return encoded.length === 0 ? route : `${route}?${encoded}`;
}

function packetFromWire(wire: InvestigationEvidencePacketWire): InvestigationEvidencePacket {
  return {
    answer: objectOrEmpty(wire.answer),
    bounds: objectOrEmpty(wire.bounds),
    graphAnswers: objectList(wire.graph_answers),
    identity: objectOrEmpty(wire.identity),
    missingEvidence: objectList(wire.missing_evidence),
    packetId: str(wire.packet_id),
    reducerDecisions: objectList(wire.reducer_decisions),
    redaction: objectOrEmpty(wire.redaction),
    refusal: str(wire.refusal),
    reproduce: objectList(wire.reproduce),
    schema: str(wire.schema),
    semanticObservations: objectList(wire.semantic_observations),
    sourceFacts: objectList(wire.source_facts),
    validation: objectOrEmpty(wire.validation)
  };
}

function objectOrEmpty(value: Record<string, unknown> | undefined): Record<string, unknown> {
  return value ?? {};
}

function objectList(value: readonly Record<string, unknown>[] | undefined): readonly Record<string, unknown>[] {
  return value ?? [];
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}
