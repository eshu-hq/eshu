// capabilityAccess.test.ts — TDD tests for nav capability gating.
// Uses real permission family names from go/internal/capabilitycatalog/authz.go:
//   identity_admin, roles_grants, tokens, repository_content, service_runtime,
//   cloud_iac, secrets_iam, supply_chain, docs_semantic, ask_search,
//   operations_status, audit_export, admin_recovery
import { describe, expect, it } from "vitest";
import type { BrowserSessionAuth } from "../api/client";
import { canAccessNav, buildAllowedNavSet } from "./capabilityAccess";

function makeAuth(overrides: Partial<BrowserSessionAuth> = {}): BrowserSessionAuth {
  return {
    mode: "browser_session",
    all_scopes: true,
    ...overrides
  };
}

describe("buildAllowedNavSet", () => {
  it("allows all nav items when all_scopes is true", () => {
    const auth = makeAuth({ all_scopes: true });
    const allowed = buildAllowedNavSet(auth);
    expect(allowed.size).toBeGreaterThan(0);
    expect(allowed.has("/dashboard")).toBe(true);
    expect(allowed.has("/catalog")).toBe(true);
    expect(allowed.has("/capabilities")).toBe(true);
    expect(allowed.has("/ask")).toBe(true);
  });

  it("allows all nav items when permission_catalog_enforced is false (fail-open)", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: false,
      allowed_permission_features: []
    });
    const allowed = buildAllowedNavSet(auth);
    expect(allowed.has("/dashboard")).toBe(true);
    expect(allowed.has("/catalog")).toBe(true);
    expect(allowed.has("/ask")).toBe(true);
  });

  it("allows all nav items when auth is null (fail-open)", () => {
    const allowed = buildAllowedNavSet(null);
    expect(allowed.has("/dashboard")).toBe(true);
    expect(allowed.has("/catalog")).toBe(true);
    expect(allowed.has("/ask")).toBe(true);
  });

  it("allows routes mapped to permission families the session has", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["ask_search", "service_runtime"]
    });
    const allowed = buildAllowedNavSet(auth);
    expect(allowed.has("/ask")).toBe(true);       // ask_search
    expect(allowed.has("/catalog")).toBe(true);   // service_runtime
    expect(allowed.has("/impact")).toBe(true);    // service_runtime
  });

  it("hides routes whose permission family is not granted", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["ask_search"]
    });
    const allowed = buildAllowedNavSet(auth);
    expect(allowed.has("/ask")).toBe(true);      // ask_search ✓
    expect(allowed.has("/sbom")).toBe(false);    // supply_chain ✗
    expect(allowed.has("/iac")).toBe(false);     // cloud_iac ✗
    expect(allowed.has("/secrets-iam")).toBe(false); // secrets_iam ✗
  });

  it("always allows core navigation (/, /status, /dashboard) regardless of scopes", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: []
    });
    const allowed = buildAllowedNavSet(auth);
    expect(allowed.has("/")).toBe(true);
    expect(allowed.has("/status")).toBe(true);
    expect(allowed.has("/dashboard")).toBe(true);
  });

  it("supply_chain family grants sbom, vulnerabilities, images, dependencies, findings, ci-cd", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["supply_chain"]
    });
    const allowed = buildAllowedNavSet(auth);
    expect(allowed.has("/sbom")).toBe(true);
    expect(allowed.has("/vulnerabilities")).toBe(true);
    expect(allowed.has("/images")).toBe(true);
    expect(allowed.has("/dependencies")).toBe(true);
    expect(allowed.has("/findings")).toBe(true);
    expect(allowed.has("/ci-cd/run-correlations")).toBe(true);
  });
});

describe("canAccessNav", () => {
  it("returns true for always-allowed route regardless of auth", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: []
    });
    expect(canAccessNav("/dashboard", auth)).toBe(true);
    expect(canAccessNav("/status", auth)).toBe(true);
    expect(canAccessNav("/", auth)).toBe(true);
  });

  it("returns true for unknown route (fail-open)", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: []
    });
    expect(canAccessNav("/some-unknown-route", auth)).toBe(true);
  });

  it("returns false for mapped route when family not granted", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["ask_search"]
    });
    expect(canAccessNav("/sbom", auth)).toBe(false);
  });

  it("returns true for mapped route when family is granted", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["supply_chain"]
    });
    expect(canAccessNav("/sbom", auth)).toBe(true);
  });
});
