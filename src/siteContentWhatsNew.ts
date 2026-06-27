import type { WhatsNewItem } from "./siteContentTypes";

/** Newly shipped surfaces that materially changed the launch story. */
export const whatsNew = [
  {
    title: "Ask Eshu",
    summary: "A self-hosted, provider-portable answer surface over the graph.",
    detail:
      "Ask Eshu uses bounded retrieval, evidence-backed answers, per-token streaming, and a default-off provider profile path instead of shipping prompts to a vendor by default.",
  },
  {
    title: "Evidence packets v2",
    summary: "portable proof artifacts across CLI, API, MCP, and console.",
    detail:
      "Investigation packets carry source facts, reducer decisions, graph/query truth, missing evidence, freshness, and reproduce handles for supply-chain impact, deployable-unit truth, and drift.",
  },
  {
    title: "Generated capability matrix",
    summary: "Capability truth is generated and fails on drift.",
    detail:
      "The catalog reconciles commands, collectors, reducer domains, API, MCP, and console surfaces so launch claims stay tied to committed code and proof signals.",
  },
  {
    title: "Pre-change developer workflow",
    summary: "Evidence-backed plans before code is modified.",
    detail:
      "`eshu change plan` produces a read-only developer_change_plan.v1 artifact with ordered actions, patch guidance, tests, missing evidence, and bounded next calls.",
  },
  {
    title: "Competitive parity gate",
    summary: "Buyer diligence moved from copy to CI-verified proof.",
    detail:
      "`eshu competitive-parity validate` scores report, proof-bundle, and capability surfaces against named peer baselines so parity is reproducible.",
  },
  {
    title: "Operator control plane",
    summary: "The 3 AM path is now part of the product story.",
    detail:
      "Unified status, freshness causality, safe deadletter replay, and runbook-oriented evidence give operators a way to diagnose the graph without guessing.",
  },
] satisfies readonly WhatsNewItem[];
