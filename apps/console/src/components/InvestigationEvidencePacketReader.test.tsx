import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { InvestigationEvidencePacket } from "../api/investigationPacket";
import { InvestigationEvidencePacketReader } from "./InvestigationEvidencePacketReader";

describe("InvestigationEvidencePacketReader", () => {
  it("renders packet layers, bounds, redaction, and reproduce handles", () => {
    render(<InvestigationEvidencePacketReader packet={packet()} />);

    expect(screen.getByRole("heading", { name: "Investigation packet" })).toBeInTheDocument();
    expect(screen.getByText("investigation-evidence-packet:demo")).toBeInTheDocument();
    expect(screen.getByText("Source facts")).toBeInTheDocument();
    expect(screen.getByText("fact:1")).toBeInTheDocument();
    expect(screen.getByText("Reducer decisions")).toBeInTheDocument();
    expect(screen.getByText("decision:1")).toBeInTheDocument();
    expect(screen.getByText("Graph answers")).toBeInTheDocument();
    expect(screen.getByText("graph:1")).toBeInTheDocument();
    expect(screen.getByText("Missing evidence")).toBeInTheDocument();
    expect(screen.getByText("runtime_image_identity")).toBeInTheDocument();
    expect(screen.getByText("Reproduce handles")).toBeInTheDocument();
    expect(screen.getByText("/api/v0/investigations/supply-chain/impact/packet")).toBeInTheDocument();
    expect(screen.getAllByText("share_safe_v2").length).toBeGreaterThan(0);
  });
});

function packet(): InvestigationEvidencePacket {
  return {
    answer: { summary: "CVE impact is admitted.", supported: true, truthClass: "exact" },
    bounds: { max_source_facts: 200, truncated: false },
    graphAnswers: [{ id: "graph:1", present: true, relation: "AFFECTED_BY" }],
    identity: { family: "supply_chain_impact", scope: { cve_id: "CVE-2026-0001" } },
    missingEvidence: [{ hop: "runtime_image_identity", reason: "image evidence missing" }],
    packetId: "investigation-evidence-packet:demo",
    reducerDecisions: [{ id: "decision:1", state: "admitted" }],
    redaction: { profile: "share_safe_v2" },
    refusal: "none",
    reproduce: [{ kind: "http", route: "/api/v0/investigations/supply-chain/impact/packet" }],
    schema: "investigation_evidence_packet.v2",
    semanticObservations: [],
    sourceFacts: [{ evidence_family: "supply_chain", fact_id: "fact:1" }],
    validation: { valid: true }
  };
}
