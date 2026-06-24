import { readFileSync } from "node:fs";
import { resolve } from "node:path";

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
    expect(model.sbom).toBeNull();
    expect(model.images).toHaveLength(0);
    expect(model.findings).toHaveLength(0);
    expect(model.graph.nodes).toHaveLength(0);
    // Demo-only time-series must not appear without a metrics endpoint.
    expect(model.series.ingestRate).toHaveLength(0);
  });

  it("stamps every section provenance when a state is provided", () => {
    const snap = emptySnapshot("unavailable");
    for (const section of ["runtime", "services", "languages", "ingesters", "findings", "vulnerabilities", "sbom", "images"]) {
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

  it("documents that route pages own live graph hydration", () => {
    const source = readFileSync(resolve(import.meta.dirname, "liveModel.ts"), "utf8");

    expect(source).toContain("Route pages hydrate their graph views directly");
    expect(source).not.toContain("relationships are not yet provided by the API");
  });
});
