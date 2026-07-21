// Unit tests for the ESHU_E2E_MCP_MODULE selector logic (F-9, issue #5170).
// This is exactly where a false-green is most dangerous: a selector that
// greens WITHOUT running the requested module (an unknown name, or a module
// whose prerequisites never ran, so it is silently skipped) makes the proof
// gate lie. These cover the pure validation + zero-step guard without a stack.
import { describe, expect, it } from "vitest";

import {
  KNOWN_MCP_E2E_MODULES,
  selectedModuleContributedSteps,
  validateSelectedModule,
} from "./authMcpE2EModuleSelection.ts";

describe("validateSelectedModule", () => {
  it("accepts the empty selection (full suite)", () => {
    expect(validateSelectedModule("").ok).toBe(true);
  });

  it("accepts every known module name", () => {
    for (const m of KNOWN_MCP_E2E_MODULES) {
      expect(validateSelectedModule(m).ok).toBe(true);
    }
  });

  it("rejects an unknown module and names the accepted set", () => {
    const result = validateSelectedModule("bogus");
    expect(result.ok).toBe(false);
    expect(result.error).toContain("bogus");
    // The message must enumerate the real options so an operator can self-correct.
    for (const m of KNOWN_MCP_E2E_MODULES) {
      expect(result.error).toContain(m);
    }
  });

  it("rejects near-miss / casing typos (exact match only)", () => {
    for (const typo of ["ShapeA", "shape-a", "leakage ", "leakages", "all"]) {
      expect(validateSelectedModule(typo).ok).toBe(false);
    }
  });
});

describe("selectedModuleContributedSteps", () => {
  it("is true for the empty (full-suite) selection regardless of steps", () => {
    expect(selectedModuleContributedSteps("", [])).toBe(true);
    expect(selectedModuleContributedSteps("", ["bootstrap_setup_wizard_renders"])).toBe(true);
  });

  it("is true when the selected module produced at least one of its own steps", () => {
    expect(
      selectedModuleContributedSteps("shapeA", ["bootstrap_x", "seed_graph_repository", "shapeA_discovery_404"]),
    ).toBe(true);
    expect(selectedModuleContributedSteps("leakage", ["leakage_credentialless_probes_do_not_leak"])).toBe(true);
  });

  it("is FALSE when a selected module ran ZERO of its steps (the false-green case)", () => {
    // A lone `--module leakage` that got silently skipped: the report has only
    // bootstrap/seed steps and would otherwise green.
    const skippedLeakageReport = ["bootstrap_setup_wizard_renders", "bootstrap_setup_wizard_completes", "seed_graph_repository"];
    expect(selectedModuleContributedSteps("leakage", skippedLeakageReport)).toBe(false);
    expect(selectedModuleContributedSteps("shapeB", skippedLeakageReport)).toBe(false);
  });

  it("does not confuse one module's steps for another (prefix is boundary-anchored)", () => {
    // shapeB steps must not satisfy a shapeA selection, and vice versa.
    expect(selectedModuleContributedSteps("shapeA", ["shapeB_admin_drawer_provider_crud"])).toBe(false);
    expect(selectedModuleContributedSteps("shapeC", ["shapeB_admin_drawer_provider_crud"])).toBe(false);
  });
});
