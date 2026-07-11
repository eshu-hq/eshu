// api/queryPlaybooks.ts
// Loaders for the live guided-questions surface (issue #4746): the deterministic
// query playbook catalog (GET /api/v0/query-playbooks) and its resolver
// (POST /api/v0/query-playbooks/resolve). Both endpoints read only the
// in-process playbook catalog -- never Postgres or a graph backend -- and this
// module only normalizes their envelope payloads to the console's camelCase
// view model. See go/internal/query/query_playbook.go and
// query_playbook_handler.go for the wire contract.
import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, type EshuTruth } from "./envelope";

export type PlaybookInputType = "string" | "identifier";

export interface PlaybookInput {
  readonly name: string;
  readonly type: PlaybookInputType;
  readonly required: boolean;
  readonly description: string;
}

export interface PlaybookDrilldown {
  readonly tool: string;
  readonly reason: string;
}

export interface PlaybookStep {
  readonly id: string;
  readonly tool: string;
  readonly expectedTruth: string;
  readonly evidenceExpected: string;
  readonly drilldowns: readonly PlaybookDrilldown[];
}

export interface PlaybookFailureMode {
  readonly condition: string;
  readonly meaning: string;
  readonly fallback: string;
}

// QueryPlaybook is the console view of one catalog entry: a deterministic,
// bounded, versioned workflow description. It is data, not executable code.
export interface QueryPlaybook {
  readonly id: string;
  readonly name: string;
  readonly version: string;
  readonly promptFamily: string;
  readonly description: string;
  readonly requiredInputs: readonly PlaybookInput[];
  readonly steps: readonly PlaybookStep[];
  readonly failureModes: readonly PlaybookFailureMode[];
}

export interface PlaybookVersionRef {
  readonly id: string;
  readonly version: string;
}

// ResolvedCall is one fully specified, bounded call produced by resolving a
// playbook step against concrete inputs.
export interface ResolvedCall {
  readonly stepId: string;
  readonly tool: string;
  readonly arguments: Readonly<Record<string, unknown>>;
  readonly expectedTruth: string;
  readonly evidenceExpected: string;
  readonly drilldowns: readonly PlaybookDrilldown[];
}

export interface ResolvedPlaybook {
  readonly playbookId: string;
  readonly version: string;
  readonly promptFamily: string;
  readonly calls: readonly ResolvedCall[];
  readonly failureModes: readonly PlaybookFailureMode[];
}

// PlaybookCatalogPage is the loaded catalog plus its truth and provenance
// metadata, mirroring the CapabilityCatalogPage / RelationshipsCatalog loaders.
export interface PlaybookCatalogPage {
  readonly playbooks: readonly QueryPlaybook[];
  readonly versions: readonly PlaybookVersionRef[];
  readonly count: number;
  readonly truth: EshuTruth | null;
  readonly provenance: "live" | "empty" | "unavailable";
}

export interface PlaybookResolution {
  readonly resolved: ResolvedPlaybook;
  readonly truth: EshuTruth | null;
}

// ---------------------------------------------------------------------------
// Wire shapes (Go json tags, snake_case). Kept private: callers only see the
// normalized camelCase view model above.
// ---------------------------------------------------------------------------

interface DrilldownWire {
  readonly tool?: string;
  readonly reason?: string;
}

interface FailureModeWire {
  readonly condition?: string;
  readonly meaning?: string;
  readonly fallback?: string;
}

interface InputWire {
  readonly name?: string;
  readonly type?: string;
  readonly required?: boolean;
  readonly description?: string;
}

interface StepWire {
  readonly id?: string;
  readonly tool?: string;
  readonly expected_truth?: string;
  readonly evidence_expected?: string;
  readonly drilldowns?: readonly DrilldownWire[];
}

interface PlaybookWire {
  readonly id?: string;
  readonly name?: string;
  readonly version?: string;
  readonly prompt_family?: string;
  readonly description?: string;
  readonly required_inputs?: readonly InputWire[];
  readonly steps?: readonly StepWire[];
  readonly failure_modes?: readonly FailureModeWire[];
}

interface VersionRefWire {
  readonly id?: string;
  readonly version?: string;
}

interface ListResponseWire {
  readonly playbooks?: readonly PlaybookWire[];
  readonly versions?: readonly VersionRefWire[];
  readonly count?: number;
}

interface ResolvedCallWire {
  readonly step_id?: string;
  readonly tool?: string;
  readonly arguments?: Readonly<Record<string, unknown>>;
  readonly expected_truth?: string;
  readonly evidence_expected?: string;
  readonly drilldowns?: readonly DrilldownWire[];
}

interface ResolvedPlaybookWire {
  readonly playbook_id?: string;
  readonly version?: string;
  readonly prompt_family?: string;
  readonly calls?: readonly ResolvedCallWire[];
  readonly failure_modes?: readonly FailureModeWire[];
}

interface ResolveResponseWire {
  readonly resolved?: ResolvedPlaybookWire;
}

function normalizeDrilldown(wire: DrilldownWire): PlaybookDrilldown {
  return { tool: str(wire.tool), reason: str(wire.reason) };
}

function normalizeFailureMode(wire: FailureModeWire): PlaybookFailureMode {
  return {
    condition: str(wire.condition),
    meaning: str(wire.meaning),
    fallback: str(wire.fallback),
  };
}

function normalizeInput(wire: InputWire): PlaybookInput {
  const type: PlaybookInputType = wire.type === "identifier" ? "identifier" : "string";
  return {
    name: str(wire.name),
    type,
    required: wire.required === true,
    description: str(wire.description),
  };
}

function normalizeStep(wire: StepWire): PlaybookStep {
  return {
    id: str(wire.id),
    tool: str(wire.tool),
    expectedTruth: str(wire.expected_truth),
    evidenceExpected: str(wire.evidence_expected),
    drilldowns: (wire.drilldowns ?? []).map(normalizeDrilldown),
  };
}

function normalizePlaybook(wire: PlaybookWire): QueryPlaybook {
  return {
    id: str(wire.id),
    name: str(wire.name) || str(wire.id),
    version: str(wire.version),
    promptFamily: str(wire.prompt_family),
    description: str(wire.description),
    requiredInputs: (wire.required_inputs ?? []).map(normalizeInput),
    steps: (wire.steps ?? []).map(normalizeStep),
    failureModes: (wire.failure_modes ?? []).map(normalizeFailureMode),
  };
}

function normalizeVersionRef(wire: VersionRefWire): PlaybookVersionRef {
  return { id: str(wire.id), version: str(wire.version) };
}

function normalizeResolvedCall(wire: ResolvedCallWire): ResolvedCall {
  return {
    stepId: str(wire.step_id),
    tool: str(wire.tool),
    arguments: wire.arguments ?? {},
    expectedTruth: str(wire.expected_truth),
    evidenceExpected: str(wire.evidence_expected),
    drilldowns: (wire.drilldowns ?? []).map(normalizeDrilldown),
  };
}

function normalizeResolvedPlaybook(wire: ResolvedPlaybookWire): ResolvedPlaybook {
  return {
    playbookId: str(wire.playbook_id),
    version: str(wire.version),
    promptFamily: str(wire.prompt_family),
    calls: (wire.calls ?? []).map(normalizeResolvedCall),
    failureModes: (wire.failure_modes ?? []).map(normalizeFailureMode),
  };
}

// listPlaybooks fetches the deterministic query playbook catalog. It never
// fabricates data: any request or envelope failure resolves to an
// "unavailable" provenance so the page renders a truthful empty state instead
// of throwing.
export async function listPlaybooks(client: EshuApiClient): Promise<PlaybookCatalogPage> {
  try {
    const env = await client.get<ListResponseWire>("/api/v0/query-playbooks");
    if (env.error) throw new EshuEnvelopeError(env.error);
    const playbooks = (env.data?.playbooks ?? [])
      .map(normalizePlaybook)
      .filter((playbook) => playbook.id !== "");
    return {
      playbooks,
      versions: (env.data?.versions ?? []).map(normalizeVersionRef),
      count: env.data?.count ?? playbooks.length,
      truth: env.truth ?? null,
      provenance: playbooks.length > 0 ? "live" : "empty",
    };
  } catch {
    return { playbooks: [], versions: [], count: 0, truth: null, provenance: "unavailable" };
  }
}

// resolvePlaybook resolves one playbook against concrete inputs into its
// ordered, fully specified bounded tool calls. Unlike listPlaybooks, a failure
// here (an unknown playbook_id, a missing required input) is a specific,
// user-actionable error, so it propagates as a thrown EshuEnvelopeError /
// EshuApiHttpError rather than collapsing to a generic unavailable state.
export async function resolvePlaybook(
  client: EshuApiClient,
  request: { readonly playbookId: string; readonly inputs: Readonly<Record<string, string>> },
): Promise<PlaybookResolution> {
  const env = await client.post<ResolveResponseWire>("/api/v0/query-playbooks/resolve", {
    playbook_id: request.playbookId,
    inputs: request.inputs,
  });
  if (env.error) throw new EshuEnvelopeError(env.error);
  if (!env.data?.resolved) {
    throw new Error("query playbook resolve returned no resolved plan");
  }
  return { resolved: normalizeResolvedPlaybook(env.data.resolved), truth: env.truth ?? null };
}

function str(value: string | undefined): string {
  return value?.trim() ?? "";
}
