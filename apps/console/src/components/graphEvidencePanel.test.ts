import { graphEdgeEvidencePanelData, graphNodeEvidencePanelData } from "./graphEvidencePanel";
import type { GraphEdge, GraphNode } from "../console/types";

const node: GraphNode = {
  id: "svc:payments",
  kind: "service",
  label: "payments",
  sub: "billing-platform",
  col: 1,
  truth: "derived",
  source: { repoId: "repo-1", filePath: "cmd/main.go", startLine: 10, endLine: 20 }
};

const edge: GraphEdge = {
  s: "svc:payments",
  t: "lib:billing",
  verb: "IMPORTS",
  layer: "code",
  evidence: ["import in cmd/main.go", "go.mod require"],
  confidenceTier: "high",
  truthState: "derived",
  sourceFamily: "code_import",
  method: "ast_scan"
};

describe("graphNodeEvidencePanelData", () => {
  it("maps a node into panel data with truth label, facts, and source link", () => {
    const data = graphNodeEvidencePanelData(node);
    expect(data.kindLabel).toBe("Node evidence");
    expect(data.title).toBe("payments");
    expect(data.truthLabel).toBe("derived");
    expect(data.facts).toContainEqual({ label: "Kind", value: "service" });
    expect(data.facts).toContainEqual({ label: "Detail", value: "billing-platform" });
    expect(data.sourceHref).toBe("/repositories/repo-1/source?path=cmd%2Fmain.go&lineStart=10&lineEnd=20");
    expect(data.sourceLabel).toBe("cmd/main.go:10-20");
  });

  it("leaves the truth label empty when the node carries no truth", () => {
    const data = graphNodeEvidencePanelData({ id: "x", kind: "repo", label: "x", col: 0 });
    expect(data.truthLabel).toBe("");
    expect(data.sourceHref).toBeUndefined();
  });
});

describe("graphEdgeEvidencePanelData", () => {
  it("maps an edge into panel data with relationship facts and evidence", () => {
    const data = graphEdgeEvidencePanelData(edge, "payments", "billing");
    expect(data.kindLabel).toBe("Edge evidence");
    expect(data.title).toBe("IMPORTS");
    // truthState drives the chip vocabulary for the relationship.
    expect(data.truthLabel).toBe("derived");
    expect(data.facts).toContainEqual({ label: "From", value: "payments" });
    expect(data.facts).toContainEqual({ label: "To", value: "billing" });
    expect(data.facts).toContainEqual({ label: "Layer", value: "code" });
    expect(data.evidence).toEqual(["import in cmd/main.go", "go.mod require"]);
  });

  it("exposes confidence, source family, and method as a provenance section", () => {
    const data = graphEdgeEvidencePanelData(edge, "payments", "billing");
    const provenance = (data.sections ?? []).find((section) => section.title === "Relationship truth");
    expect(provenance?.rows).toContainEqual({ label: "Confidence", value: "high" });
    expect(provenance?.rows).toContainEqual({ label: "Source family", value: "code_import" });
    expect(provenance?.rows).toContainEqual({ label: "Method", value: "ast_scan" });
  });

  it("keeps an unsupported relationship explicit rather than blank", () => {
    const data = graphEdgeEvidencePanelData(
      { s: "a", t: "b", verb: "RELATED", layer: "ops" },
      "a",
      "b"
    );
    expect(data.truthLabel).toBe("");
    const provenance = (data.sections ?? []).find((section) => section.title === "Relationship truth");
    // No provenance rows means the section is omitted, not rendered empty.
    expect(provenance).toBeUndefined();
  });
});
