import type { EshuEnvelope, EshuTruth } from "./envelope";
import { unwrapEnvelope } from "./envelope";

export function envelopePayload<TData>(response: unknown): {
  readonly data: TData;
  readonly truth?: EshuTruth;
} {
  if (isEnvelope(response)) {
    return unwrapEnvelope(response);
  }
  return {
    data: response as TData
  };
}

function isEnvelope<TData>(response: unknown): response is EshuEnvelope<TData> {
  if (typeof response !== "object" || response === null) {
    return false;
  }
  return "data" in response || "truth" in response || "error" in response;
}
