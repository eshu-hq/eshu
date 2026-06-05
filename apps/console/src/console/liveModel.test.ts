import { describe, expect, it } from "vitest";
import { emptyConsoleModel, emptySnapshot, modelFromSnapshot } from "./liveModel";

describe("liveModel", () => {
  it("emptyConsoleModel is live-only with no fabricated data", () => {
    const model = emptyConsoleModel();
    expect(model.source).toBe("live");
    expect(model.runtime.indexStatus).toBe("unavailable");
    expect(model.runtime.repositories).toBe(0);
    expect(model.services).toHaveLength(0);
    expect(model.vulnerabilities).toHaveLength(0);
    expect(model.findings).toHaveLength(0);
    expect(model.graph.nodes).toHaveLength(0);
    // Demo-only time-series must not appear without a metrics endpoint.
    expect(model.series.ingestRate).toHaveLength(0);
  });

  it("stamps every section provenance when a state is provided", () => {
    const snap = emptySnapshot("unavailable");
    for (const section of ["runtime", "services", "languages", "ingesters", "findings", "vulnerabilities"]) {
      expect(snap.provenance[section]).toBe("unavailable");
    }
  });

  it("leaves provenance empty by default (initial loading state)", () => {
    expect(Object.keys(emptySnapshot().provenance)).toHaveLength(0);
  });

  it("modelFromSnapshot marks the model as live", () => {
    const model = modelFromSnapshot(emptySnapshot());
    expect(model.source).toBe("live");
  });
});
