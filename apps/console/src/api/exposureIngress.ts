// api/exposureIngress.ts
// Loader for the entrypoint-first exposure view. It reads the bounded service
// context served at GET /api/v0/services/{name}/context and assembles the proven
// internet ingress chain (entrypoint -> edge/CDN -> runtime/service) plus the
// WAF/TLS posture, each hop carrying an honest truth level.
//
// It never fabricates a chain. The synthetic "Internet" origin hop is drawn only
// when an entrypoint's visibility is observed public; an internal entrypoint gets
// a "Network boundary" origin, mirroring the conservative chainOrigin discipline
// the handler-trace view already uses. WAF/TLS posture is three-valued
// (protected/unprotected/unproven and terminated/not_terminated/unproven) so an
// observed-negative is never confused with missing evidence.
import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, type EshuTruth } from "./envelope";

// IngressTruth is the per-hop truth level. It mirrors the repo's evidence
// vocabulary: "observed" for scanned cloud state, "derived" for assembled links,
// "unresolved" when the hop's evidence is not materialized.
export type IngressTruth = "observed" | "derived" | "unresolved";

// IngressPostureState is the three-valued posture for a WAF/TLS tile.
export type IngressPostureState =
  | "protected"
  | "unprotected"
  | "terminated"
  | "not_terminated"
  | "unproven";

// IngressHop is one node on the proven ingress chain. Each hop is clickable in
// the view and opens its evidence panel.
export interface IngressHop {
  readonly id: string;
  readonly kind: string;
  readonly label: string;
  readonly detail: string;
  readonly truth: IngressTruth;
  readonly reason: string;
}

// IngressChain is one entrypoint's proven path to the runtime/service.
export interface IngressChain {
  readonly entrypoint: string;
  readonly visibility: string;
  readonly hops: readonly IngressHop[];
}

// IngressPosture is the WAF/TLS posture summary for the service's edge resources.
export interface IngressPosture {
  readonly wafCoverage: IngressPostureState;
  readonly tlsTermination: IngressPostureState;
  readonly edgeCount: number;
  readonly wafProtected: number;
  readonly tlsTerminated: number;
  readonly reason: string;
}

// ExposureIngress is the normalized view-model for one service's exposure.
export interface ExposureIngress {
  readonly service: string;
  readonly chains: readonly IngressChain[];
  readonly posture: IngressPosture;
  readonly publicEntrypoints: number;
  readonly totalHops: number;
  readonly truth: EshuTruth | null;
  readonly provenance: "live" | "empty" | "unavailable";
  readonly error?: string;
}

interface EntrypointWire {
  readonly type?: string;
  readonly target?: string;
  readonly environment?: string;
  readonly visibility?: string;
  readonly reason?: string;
}

interface NetworkPathWire {
  readonly path_type?: string;
  readonly from_type?: string;
  readonly from?: string;
  readonly to_type?: string;
  readonly to?: string;
  readonly platform_kind?: string;
  readonly environment?: string;
  readonly visibility?: string;
  readonly reason?: string;
}

interface IngressPostureWire {
  readonly waf_coverage?: string;
  readonly tls_termination?: string;
  readonly edge_count?: number;
  readonly waf_protected?: number;
  readonly tls_terminated?: number;
  readonly reason?: string;
}

interface ServiceContextWire {
  readonly name?: string;
  readonly entrypoints?: readonly EntrypointWire[] | null;
  readonly network_paths?: readonly NetworkPathWire[] | null;
  readonly ingress_posture?: IngressPostureWire;
}

const postureStates: ReadonlySet<IngressPostureState> = new Set([
  "protected",
  "unprotected",
  "terminated",
  "not_terminated",
  "unproven"
]);

// parsePostureState defaults an unknown wire value to "unproven" so a tile never
// optimistically claims protection it cannot prove.
function parsePostureState(raw: string | undefined): IngressPostureState {
  return raw !== undefined && postureStates.has(raw as IngressPostureState)
    ? (raw as IngressPostureState)
    : "unproven";
}

function postureFromWire(wire: IngressPostureWire | undefined): IngressPosture {
  return {
    wafCoverage: parsePostureState(wire?.waf_coverage),
    tlsTermination: parsePostureState(wire?.tls_termination),
    edgeCount: wire?.edge_count ?? 0,
    wafProtected: wire?.waf_protected ?? 0,
    tlsTerminated: wire?.tls_terminated ?? 0,
    reason: wire?.reason ?? ""
  };
}

// originHop returns the synthetic leading hop for an entrypoint's visibility, or
// null when no origin should be drawn. It must never over-claim public
// reachability: only an observed-public entrypoint gets an "Internet" origin; an
// internal entrypoint gets a truthful "Network boundary" origin.
function originHop(visibility: string): IngressHop | null {
  if (visibility === "public") {
    return {
      id: "origin:internet",
      kind: "internet",
      label: "Internet",
      detail: "0.0.0.0/0",
      truth: "observed",
      reason: "entrypoint is an observed public hostname"
    };
  }
  if (visibility === "internal") {
    return {
      id: "origin:network",
      kind: "network",
      label: "Network boundary",
      detail: "internal",
      truth: "observed",
      reason: "entrypoint is internal; public reachability is not claimed"
    };
  }
  return null;
}

// buildChains assembles one ingress chain per network path. Each chain begins
// with an honest origin hop (or none), then the entrypoint, then the runtime
// target. The network path link is "derived" (assembled from entrypoint +
// runtime evidence); the entrypoint and runtime nodes carry the visibility the
// backend observed.
function buildChains(
  entrypoints: readonly EntrypointWire[],
  paths: readonly NetworkPathWire[]
): readonly IngressChain[] {
  if (paths.length === 0) {
    return [];
  }
  const visibilityByTarget = new Map<string, string>();
  for (const entry of entrypoints) {
    if (entry.target) {
      visibilityByTarget.set(entry.target, entry.visibility ?? "");
    }
  }

  return paths.map((path) => {
    const entrypoint = path.from ?? "";
    const visibility = visibilityByTarget.get(entrypoint) ?? path.visibility ?? "";
    const hops: IngressHop[] = [];
    const origin = originHop(visibility);
    if (origin !== null) {
      hops.push(origin);
    }
    hops.push({
      id: `entrypoint:${entrypoint}`,
      kind: path.from_type ?? "entrypoint",
      label: humanizeKind(path.from_type ?? "entrypoint"),
      detail: entrypoint,
      truth: "observed",
      reason: path.reason ?? "observed entrypoint"
    });
    hops.push({
      id: `runtime:${path.to ?? ""}`,
      kind: path.to_type ?? "runtime",
      label: runtimeLabel(path),
      detail: path.to ?? "",
      truth: "derived",
      reason:
        path.reason ??
        "runtime target derived from the entrypoint-to-runtime network path"
    });
    return { entrypoint, visibility, hops };
  });
}

function runtimeLabel(path: NetworkPathWire): string {
  const kind = path.platform_kind ?? path.to_type ?? "runtime";
  return humanizeKind(kind);
}

function humanizeKind(value: string): string {
  return value
    .replace(/_/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase())
    .replace(/\bEks\b/g, "EKS")
    .replace(/\bEcs\b/g, "ECS")
    .replace(/\bAws\b/g, "AWS")
    .replace(/\bCdn\b/g, "CDN");
}

function ingressFromWire(wire: ServiceContextWire, fallbackName: string, truth: EshuTruth | null): ExposureIngress {
  const entrypoints = wire.entrypoints ?? [];
  const paths = wire.network_paths ?? [];
  const chains = buildChains(entrypoints, paths);
  const posture = postureFromWire(wire.ingress_posture);
  const publicEntrypoints = entrypoints.filter((entry) => entry.visibility === "public").length;
  const totalHops = chains.reduce((sum, chain) => sum + chain.hops.length, 0);
  return {
    service: wire.name ?? fallbackName,
    chains,
    posture,
    publicEntrypoints,
    totalHops,
    truth,
    provenance: chains.length > 0 ? "live" : "empty"
  };
}

// loadExposureIngress fetches the proven ingress chain and posture for a service.
// On any failure it returns an "unavailable" provenance carrying the error
// message, never a fabricated chain.
export async function loadExposureIngress(
  client: EshuApiClient,
  service: string
): Promise<ExposureIngress> {
  const name = service.trim();
  if (name.length === 0) {
    return emptyIngress(name, "A service name is required to trace its ingress chain.");
  }
  try {
    const env = await client.get<ServiceContextWire>(
      `/api/v0/services/${encodeURIComponent(name)}/context`
    );
    if (env.error !== null) {
      throw new EshuEnvelopeError(env.error);
    }
    if (env.data === null) {
      throw new Error("Eshu envelope success response is missing data");
    }
    return ingressFromWire(env.data, name, env.truth ?? null);
  } catch (error) {
    return emptyIngress(name, error instanceof Error ? error.message : "request failed");
  }
}

function emptyIngress(service: string, message: string): ExposureIngress {
  return {
    service,
    chains: [],
    posture: {
      wafCoverage: "unproven",
      tlsTermination: "unproven",
      edgeCount: 0,
      wafProtected: 0,
      tlsTerminated: 0,
      reason: ""
    },
    publicEntrypoints: 0,
    totalHops: 0,
    truth: null,
    provenance: "unavailable",
    error: message
  };
}
