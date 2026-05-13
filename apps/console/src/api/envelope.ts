export type TruthLevel = "exact" | "derived" | "fallback";

export type FreshnessState = "fresh" | "stale" | "building" | "unavailable";

export type RuntimeProfile =
  | "local_lightweight"
  | "local_authoritative"
  | "local_full_stack"
  | "production";

export interface EshuFreshness {
  readonly state: FreshnessState;
}

export interface EshuTruth {
  readonly basis?: string;
  readonly capability: string;
  readonly freshness: EshuFreshness;
  readonly level: TruthLevel;
  readonly profile: RuntimeProfile;
  readonly reason?: string;
}

export interface EshuError {
  readonly code: string;
  readonly message: string;
  readonly capability?: string;
  readonly profiles?: {
    readonly current?: string;
    readonly required?: string;
  };
}

export interface EshuEnvelope<TData> {
  readonly data: TData | null;
  readonly error: EshuError | null;
  readonly truth: EshuTruth | null;
}

export interface UnwrappedEnvelope<TData> {
  readonly data: TData;
  readonly truth: EshuTruth;
}

export class EshuEnvelopeError extends Error {
  readonly error: EshuError;

  constructor(error: EshuError) {
    super(`${error.code}: ${error.message}`);
    this.name = "EshuEnvelopeError";
    this.error = error;
  }
}

export function isEshuErrorEnvelope<TData>(envelope: EshuEnvelope<TData>): boolean {
  return envelope.error !== null;
}

export function unwrapEnvelope<TData>(
  envelope: EshuEnvelope<TData>
): UnwrappedEnvelope<TData> {
  if (envelope.error !== null) {
    throw new EshuEnvelopeError(envelope.error);
  }
  if (envelope.data === null || envelope.truth === null) {
    throw new Error("Eshu envelope success response is missing data or truth");
  }
  return {
    data: envelope.data,
    truth: envelope.truth
  };
}
