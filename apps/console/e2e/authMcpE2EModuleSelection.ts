// authMcpE2EModuleSelection.ts — pure ESHU_E2E_MCP_MODULE selector logic for
// the MCP-identity E2E runner (F-9, issue #5170). Extracted from
// runAuthMcpE2E.ts so it is unit-testable without a live stack
// (authMcpE2EModuleSelection.test.ts) — this is exactly where a false-green is
// most dangerous: a selector that greens WITHOUT running the requested module
// (an unknown name, or a module whose prerequisites never ran) makes a proof
// gate lie.

// KNOWN_MCP_E2E_MODULES is the complete set of accepted ESHU_E2E_MCP_MODULE
// values (an empty/unset value means "run the full suite" and is handled by the
// caller, not listed here):
//   - shapeA / shapeC / shapeB — the three org-shape modules;
//   - leakage — the negative-leakage module (depends on shape B's state);
//   - credentialless — the standalone credential-less negative probes the
//     mutation-sensitivity gate drives (no browser/wizard/shape state).
export const KNOWN_MCP_E2E_MODULES = ["shapeA", "shapeC", "shapeB", "leakage", "credentialless"] as const;

export type McpE2EModule = (typeof KNOWN_MCP_E2E_MODULES)[number];

export interface ModuleValidationResult {
  readonly ok: boolean;
  readonly error?: string;
}

// validateSelectedModule accepts an empty string (full suite) or one of
// KNOWN_MCP_E2E_MODULES. Any other value is rejected with an actionable
// message — the caller MUST exit non-zero and write no report, so an unknown
// module can never green.
export function validateSelectedModule(selected: string): ModuleValidationResult {
  if (selected === "") {
    return { ok: true };
  }
  if ((KNOWN_MCP_E2E_MODULES as readonly string[]).includes(selected)) {
    return { ok: true };
  }
  return {
    ok: false,
    error:
      `unknown ESHU_E2E_MCP_MODULE=${selected}; accepted: ${KNOWN_MCP_E2E_MODULES.join(", ")}, ` +
      `or empty/unset for the full suite`,
  };
}

// moduleStepPrefix returns the step-id prefix a given module contributes, so a
// caller can assert the module actually produced steps. Every shape/leakage
// module names its steps "<module>_…"; credentialless is a special case (its
// single step is "leakage_credentialless_probes_do_not_leak", handled on its
// own fast path, so it is not asserted through this generic guard).
export function moduleStepPrefix(module: string): string {
  return `${module}_`;
}

// selectedModuleContributedSteps reports whether a non-empty selected module
// produced at least one of its own steps in the recorded result ids. The
// runner uses this as a general false-green guard: any selected module that
// ran ZERO of its intended steps (e.g. a lone `--module leakage` whose shape-B
// prerequisite never ran, so it was silently skipped) must fail the run, not
// write a green report. Returns true for the empty (full-suite) selection —
// that path runs everything and is not guarded here.
export function selectedModuleContributedSteps(selected: string, resultIds: readonly string[]): boolean {
  if (selected === "") {
    return true;
  }
  const prefix = moduleStepPrefix(selected);
  return resultIds.some((id) => id.startsWith(prefix));
}
