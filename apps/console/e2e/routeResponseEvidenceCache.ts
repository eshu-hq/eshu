import { createHash } from "node:crypto";

import type {
  NetworkObservation,
  RouteResponseEvidenceSource,
} from "../src/e2e/routeAssertions.ts";

export interface RouteInputControlState {
  readonly identity: string;
  readonly kind: string;
  readonly value: string;
}

export interface RouteInputState {
  readonly controls: readonly RouteInputControlState[];
  readonly pathname: string;
  readonly search: string;
}

export interface SelectedRouteResponseEvidence {
  readonly network: readonly NetworkObservation[];
  readonly source: RouteResponseEvidenceSource;
}

/** Holds successful response evidence for one live E2E browser session. */
export class RouteResponseEvidenceCache {
  private readonly successfulByKey = new Map<string, readonly NetworkObservation[]>();

  select(key: string, freshNetwork: readonly NetworkObservation[]): SelectedRouteResponseEvidence {
    if (freshNetwork.length > 0) {
      return { network: freshNetwork, source: "fresh" };
    }
    const cached = this.successfulByKey.get(key);
    return cached === undefined
      ? { network: freshNetwork, source: "fresh" }
      : { network: cached, source: "same_session_cache" };
  }

  remember(key: string, network: readonly NetworkObservation[], successful: boolean): void {
    if (network.length === 0) return;
    if (!successful) {
      this.successfulByKey.delete(key);
      return;
    }
    this.successfulByKey.set(key, [...network]);
  }
}

/** Hashes exact route and rendered input state without retaining input values. */
export function routeResponseEvidenceKey(
  routePath: string,
  workflowID: string,
  input: RouteInputState,
): string {
  return createHash("sha256")
    .update(JSON.stringify({ input, routePath, workflowID }))
    .digest("hex");
}
