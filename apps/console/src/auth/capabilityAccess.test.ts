// capabilityAccess.test.ts — TDD tests for nav capability gating.
import { describe, expect, it } from "vitest";
import type { BrowserSessionAuth } from "../api/client";
import type { CapabilityRow } from "../api/capabilityCatalog";
import { canAccessNav, buildAllowedNavSet } from "./capabilityAccess";

function makeAuth(overrides: Partial<BrowserSessionAuth> = {}): BrowserSessionAuth {
  return {
    mode: "browser_session",
    all_scopes: true,
    ...overrides
  };
}

function makeCapRow(
  capability: string,
  opts: Partial<CapabilityRow> = {}
): CapabilityRow {
  return {
    capability,
    displayName: capability,
    ownerPackage: "",
    maturity: "general_availability",
    derivedMaturity: "general_availability",
    maturityReason: "",
    surfaces: [],
    proofSignals: [],
    knownGaps: [],
    linkedIssues: [],
    console: true,
    ...opts
  };
}

describe("buildAllowedNavSet", () => {
  it("allows all nav items when all_scopes is true", () => {
    const auth = makeAuth({ all_scopes: true });
    const rows: readonly CapabilityRow[] = [];
    const allowed = buildAllowedNavSet(auth, rows);
    // all_scopes=true: every nav route should be visible
    expect(allowed.size).toBeGreaterThan(0);
    expect(allowed.has("/dashboard")).toBe(true);
    expect(allowed.has("/catalog")).toBe(true);
    expect(allowed.has("/capabilities")).toBe(true);
  });

  it("allows all nav items when catalog is empty (fail-open)", () => {
    const auth = makeAuth({ all_scopes: false, allowed_scope_ids: [] });
    const rows: readonly CapabilityRow[] = [];
    const allowed = buildAllowedNavSet(auth, rows);
    // fail-open: empty catalog means show everything since server enforces
    expect(allowed.has("/dashboard")).toBe(true);
    expect(allowed.has("/catalog")).toBe(true);
  });

  it("allows routes mapped to capabilities the user has scope for", () => {
    const auth = makeAuth({
      all_scopes: false,
      allowed_scope_ids: ["platform_inventory.service_catalog"]
    });
    const rows: readonly CapabilityRow[] = [
      makeCapRow("platform_inventory.service_catalog")
    ];
    const allowed = buildAllowedNavSet(auth, rows);
    expect(allowed.has("/catalog")).toBe(true);
  });

  it("hides routes whose capability is not in allowed_scope_ids", () => {
    const auth = makeAuth({
      all_scopes: false,
      allowed_scope_ids: ["platform_inventory.service_catalog"]
    });
    // Provide a non-empty catalog so we don't fail-open
    const rows: readonly CapabilityRow[] = [
      makeCapRow("platform_inventory.service_catalog"),
      makeCapRow("supply_chain.sbom_attestations")
    ];
    const allowed = buildAllowedNavSet(auth, rows);
    expect(allowed.has("/catalog")).toBe(true);
    // /sbom maps to supply_chain.sbom_attestations which is not in allowed_scope_ids
    expect(allowed.has("/sbom")).toBe(false);
  });

  it("always allows core navigation (/, /status, /dashboard) regardless of scopes", () => {
    const auth = makeAuth({
      all_scopes: false,
      allowed_scope_ids: []
    });
    // non-empty catalog to avoid fail-open
    const rows: readonly CapabilityRow[] = [
      makeCapRow("platform_inventory.service_catalog")
    ];
    const allowed = buildAllowedNavSet(auth, rows);
    expect(allowed.has("/")).toBe(true);
    expect(allowed.has("/status")).toBe(true);
    expect(allowed.has("/dashboard")).toBe(true);
  });
});

describe("canAccessNav", () => {
  it("returns true for allowed route", () => {
    const auth = makeAuth({ all_scopes: true });
    expect(canAccessNav("/dashboard", auth, [])).toBe(true);
  });

  it("returns true for unknown route (fail-open)", () => {
    const auth = makeAuth({ all_scopes: false, allowed_scope_ids: [] });
    // non-empty catalog — but route has no mapping → fail-open
    const rows: readonly CapabilityRow[] = [
      makeCapRow("platform_inventory.service_catalog")
    ];
    expect(canAccessNav("/some-unknown-route", auth, rows)).toBe(true);
  });
});
