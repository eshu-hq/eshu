import type {
  ReplatformingInput,
  ReplatformingOwnership,
  ReplatformingPlan,
  ReplatformingReview,
  ReplatformingRollups,
  ReplatformingScopeKind,
} from "../api/replatforming";
import type { ReplatformingSelectorInventory } from "../api/replatformingSelectors";
import type { ReplatformingFormState } from "./ReplatformingFilters";

const scopeKinds: readonly ReplatformingScopeKind[] = ["account", "region", "service"];

export function formFromSearch(params: URLSearchParams): ReplatformingFormState {
  return {
    accountId: params.get("account_id") ?? "",
    findingKinds: params.get("finding_kinds") ?? "",
    limit: params.get("limit") ?? "100",
    offset: params.get("offset") ?? "0",
    region: params.get("region") ?? "",
    scopeId: params.get("scope_id") ?? "",
    scopeKind: scopeKind(params.get("scope_kind")),
  };
}

export function inputFromForm(form: ReplatformingFormState): ReplatformingInput {
  return {
    accountId: form.accountId,
    findingKinds: form.findingKinds
      .split(",")
      .map((kind) => kind.trim())
      .filter(Boolean),
    limit: optionalNumber(form.limit),
    offset: optionalNumber(form.offset),
    region: form.region,
    scopeId: form.scopeId,
    scopeKind: form.scopeKind,
  };
}

export function searchFromForm(form: ReplatformingFormState): URLSearchParams {
  const params = new URLSearchParams();
  params.set("scope_kind", form.scopeKind);
  addParam(params, "scope_id", form.scopeId);
  addParam(params, "account_id", form.accountId);
  addParam(params, "region", form.region);
  addParam(params, "finding_kinds", form.findingKinds);
  if (form.limit.trim() !== "" && form.limit.trim() !== "100") {
    params.set("limit", form.limit.trim());
  }
  if (form.offset.trim() !== "" && form.offset.trim() !== "0") {
    params.set("offset", form.offset.trim());
  }
  return params;
}

export function hasAnchor(form: ReplatformingFormState): boolean {
  return form.accountId.trim() !== "" || form.scopeId.trim() !== "";
}

export function inventoryStatus(
  inventory: ReplatformingSelectorInventory | null,
  inventoryLoading: boolean,
  review: ReplatformingReview | null,
  busy: boolean,
): string {
  if (inventoryLoading) return "Loading selector inventory...";
  if (inventory?.readiness.state === "collector_evidence_absent") {
    return (
      inventory.readiness.detail ||
      "No active AWS collector evidence is available for replatforming review."
    );
  }
  if (inventory?.readiness.state === "no_authorized_scopes") {
    return inventory.readiness.detail || "No AWS collector scopes are authorized for this session.";
  }
  if (busy) return "Loading the bounded replatforming plan...";
  if (review === null && inventory !== null) {
    return (
      inventory.readiness.nextAction ||
      "Choose an account, region, or source scope to review a bounded plan."
    );
  }
  if (review?.rollups.status === "ready" && review.rollups.data.totalFindingsCount === 0) {
    return "The selected active collector scope is authoritative and currently has zero replatforming findings.";
  }
  return "";
}

export function optionalNumber(value: string): number | undefined {
  const trimmed = value.trim();
  if (trimmed.length === 0) return undefined;
  const parsed = Number(trimmed);
  return Number.isFinite(parsed) ? parsed : undefined;
}

export function nextReviewOffset(
  review: ReplatformingReview | null,
  currentOffset: number,
  limit: number,
): number | null {
  if (review === null) return null;
  const sections = [review.rollups, review.plan, review.ownership];
  const readyWithMore = sections.filter(
    (section) => section.status === "ready" && section.data.truncated,
  );
  if (readyWithMore.length === 0) return null;
  for (const section of readyWithMore) {
    if (section.status === "ready" && section.data.nextOffset !== null) {
      return section.data.nextOffset;
    }
  }
  return currentOffset + limit;
}

export function statRows(
  rollups: ReplatformingRollups | null,
  plan: ReplatformingPlan | null,
  ownership: ReplatformingOwnership | null,
): readonly {
  readonly color: string;
  readonly label: string;
  readonly sub: string;
  readonly value: number | string;
}[] {
  return [
    {
      color: "var(--teal)",
      label: "Findings",
      sub: "bounded rollup",
      value: rollups?.totalFindingsCount ?? "-",
    },
    {
      color: "var(--blue)",
      label: "Ready imports",
      sub: "planning only",
      value: plan?.readyImportCount ?? "-",
    },
    {
      color: "var(--crit)",
      label: "Refused",
      sub: "safety gate",
      value: plan?.refusedImportCount ?? "-",
    },
    {
      color: "var(--violet)",
      label: "Ownership packets",
      sub: "candidate owners",
      value: ownership?.packetsCount ?? "-",
    },
  ];
}

export function formatLabel(value: string): string {
  return value.replace(/_/g, " ");
}

export function classToken(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9_-]/g, "-");
}

export function shortStableId(value: string): string {
  if (value.length <= 72) return value;
  return `${value.slice(0, 69)}...`;
}

function scopeKind(value: string | null): ReplatformingScopeKind {
  return scopeKinds.includes(value as ReplatformingScopeKind)
    ? (value as ReplatformingScopeKind)
    : "account";
}

function addParam(params: URLSearchParams, key: string, value: string): void {
  const trimmed = value.trim();
  if (trimmed.length > 0) params.set(key, trimmed);
}
