import { render, screen, within } from "@testing-library/react";
import { CollectorReadinessPage } from "./CollectorReadinessPage";
import type { CollectorReadinessRow } from "../api/collectorReadiness";

describe("CollectorReadinessPage", () => {
  it("groups collectors by source family and keeps proof states distinct", () => {
    render(<CollectorReadinessPage rows={rows} provenance="live" />);

    expect(screen.getByRole("heading", { name: "Collector Readiness" })).toBeInTheDocument();
    expect(screen.getByText("GET /api/v0/status/collector-readiness")).toBeInTheDocument();

    const sourceCollection = screen.getByRole("region", { name: "Source collection" });
    expect(within(sourceCollection).getByText("Git Repository")).toBeInTheDocument();
    expect(within(sourceCollection).getByText("implemented")).toBeInTheDocument();
    expect(within(sourceCollection).getByText("42 observations")).toBeInTheDocument();
    expect(within(sourceCollection).getByText("available")).toBeInTheDocument();
    expect(within(sourceCollection).getByText("API/MCP evidence")).toBeInTheDocument();

    const cloudRuntime = screen.getByRole("region", { name: "Cloud and runtime" });
    expect(within(cloudRuntime).getByText("AWS Cloud")).toBeInTheDocument();
    expect(within(cloudRuntime).getByText("failed")).toBeInTheDocument();
    expect(within(cloudRuntime).getByText("runtime health degraded")).toBeInTheDocument();

    expect(screen.getByText("permission hidden")).toBeInTheDocument();
    expect(screen.getByText("unsupported")).toBeInTheDocument();
    expect(screen.getByText("partial")).toBeInTheDocument();
    expect(screen.getByText("stale")).toBeInTheDocument();
    expect(screen.getByText("disabled")).toBeInTheDocument();
    expect(screen.getByText("claim-driven collector registered with claims disabled")).toBeInTheDocument();
  });
});

const rows: readonly CollectorReadinessRow[] = [
  {
    blockingGate: "none",
    claimDriven: false,
    claimState: "direct",
    displayName: "Git Repository",
    evidence: ["source facts", "reducer facts", "queue", "API/MCP evidence"],
    family: "Source collection",
    health: "healthy",
    instanceId: "git-primary",
    kind: "git",
    lastProof: "42 observations",
    reducerReadback: "available",
    sourceScope: "repository",
    state: "implemented",
    stateLabel: "implemented"
  },
  {
    blockingGate: "runtime health degraded",
    claimDriven: true,
    claimState: "claim_driven",
    displayName: "AWS Cloud",
    evidence: ["source facts"],
    family: "Cloud and runtime",
    health: "degraded",
    instanceId: "aws-prod",
    kind: "aws",
    lastProof: "7 observations",
    reducerReadback: "pending",
    sourceScope: "account",
    state: "failed",
    stateLabel: "failed"
  },
  {
    blockingGate: "claim-driven collector registered with claims disabled",
    claimDriven: true,
    claimState: "direct",
    displayName: "PagerDuty",
    evidence: [],
    family: "Operations evidence",
    health: "healthy",
    instanceId: "pagerduty",
    kind: "pagerduty",
    lastProof: "not observed",
    reducerReadback: "unavailable",
    sourceScope: "pagerduty_account",
    state: "gated",
    stateLabel: "gated"
  },
  {
    blockingGate: "missing reducer readback",
    claimDriven: true,
    claimState: "claim_driven",
    displayName: "Grafana",
    evidence: ["source facts"],
    family: "Operations evidence",
    health: "degraded",
    instanceId: "grafana",
    kind: "grafana",
    lastProof: "3 observations",
    reducerReadback: "pending",
    sourceScope: "workspace",
    state: "partial",
    stateLabel: "partial"
  },
  {
    blockingGate: "last proof is older than the promotion window",
    claimDriven: true,
    claimState: "claim_driven",
    displayName: "Jira",
    evidence: ["queue"],
    family: "Operations evidence",
    health: "stale",
    instanceId: "jira",
    kind: "jira",
    lastProof: "2026-06-18T12:00:00Z",
    reducerReadback: "available",
    sourceScope: "workspace",
    state: "stale",
    stateLabel: "stale"
  },
  {
    blockingGate: "registration-only collector disabled by policy",
    claimDriven: true,
    claimState: "registration_only",
    displayName: "SBOM Attestation",
    evidence: [],
    family: "Security evidence",
    health: "disabled",
    instanceId: "sbom",
    kind: "sbom_attestation",
    lastProof: "not observed",
    reducerReadback: "unavailable",
    sourceScope: "attestation",
    state: "disabled",
    stateLabel: "disabled"
  },
  {
    blockingGate: "hidden by active permission scope",
    claimDriven: true,
    claimState: "none",
    displayName: "Kubernetes Live",
    evidence: [],
    family: "Cloud and runtime",
    health: "hidden",
    instanceId: "",
    kind: "kubernetes_live",
    lastProof: "not observed",
    reducerReadback: "unavailable",
    sourceScope: "cluster",
    state: "permission_hidden",
    stateLabel: "permission hidden"
  },
  {
    blockingGate: "no configured instance for this collector family",
    claimDriven: true,
    claimState: "none",
    displayName: "Vault Live",
    evidence: [],
    family: "Security evidence",
    health: "unsupported",
    instanceId: "",
    kind: "vault_live",
    lastProof: "not observed",
    reducerReadback: "unavailable",
    sourceScope: "vault_cluster",
    state: "unsupported",
    stateLabel: "unsupported"
  }
];
