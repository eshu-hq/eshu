// capabilityAccess.test.ts — TDD tests for nav capability gating.
// Uses real permission family names from go/internal/capabilitycatalog/authz.go:
//   identity_admin, roles_grants, tokens, repository_content, service_runtime,
//   cloud_iac, secrets_iam, supply_chain, docs_semantic, ask_search,
//   operations_status, audit_export, admin_recovery
import { describe, expect, it } from "vitest";

import {
  canAccessNav,
  buildAllowedNavSet,
  canAccessAdminRoute,
  visibleAdminPanels,
  isPermissionCatalogEnforced,
} from "./capabilityAccess";
import type { BrowserSessionAuth } from "../api/client";

function makeAuth(overrides: Partial<BrowserSessionAuth> = {}): BrowserSessionAuth {
  return {
    mode: "browser_session",
    all_scopes: true,
    ...overrides,
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
      allowed_permission_features: [],
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
      allowed_permission_features: ["ask_search", "service_runtime"],
    });
    const allowed = buildAllowedNavSet(auth);
    expect(allowed.has("/ask")).toBe(true); // ask_search
    expect(allowed.has("/catalog")).toBe(true); // service_runtime
    expect(allowed.has("/impact")).toBe(true); // service_runtime
  });

  it("hides routes whose permission family is not granted", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["ask_search"],
    });
    const allowed = buildAllowedNavSet(auth);
    expect(allowed.has("/ask")).toBe(true); // ask_search ✓
    expect(allowed.has("/sbom")).toBe(false); // supply_chain ✗
    expect(allowed.has("/iac")).toBe(false); // cloud_iac ✗
    expect(allowed.has("/secrets-iam")).toBe(false); // secrets_iam ✗
  });

  it("always allows core navigation (/, /status, /dashboard) regardless of scopes", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: [],
    });
    const allowed = buildAllowedNavSet(auth);
    expect(allowed.has("/")).toBe(true);
    expect(allowed.has("/status")).toBe(true);
    expect(allowed.has("/dashboard")).toBe(true);
  });

  it("hides /admin when identity_admin is not granted (#3703)", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["ask_search"],
    });
    const allowed = buildAllowedNavSet(auth);
    expect(allowed.has("/admin")).toBe(false);
  });

  it("shows /admin when identity_admin is granted (#3703)", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["identity_admin"],
    });
    const allowed = buildAllowedNavSet(auth);
    expect(allowed.has("/admin")).toBe(true);
  });

  it("supply_chain family grants sbom, vulnerabilities, images, dependencies, findings, ci-cd", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["supply_chain"],
    });
    const allowed = buildAllowedNavSet(auth);
    expect(allowed.has("/sbom")).toBe(true);
    expect(allowed.has("/vulnerabilities")).toBe(true);
    expect(allowed.has("/images")).toBe(true);
    expect(allowed.has("/dependencies")).toBe(true);
    expect(allowed.has("/findings")).toBe(true);
    expect(allowed.has("/ci-cd/run-correlations")).toBe(true);
  });

  it("shows /admin when only `tokens` is granted (#4969 partial grant)", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["tokens"],
    });
    expect(buildAllowedNavSet(auth).has("/admin")).toBe(true);
  });

  it("shows /admin when only `audit_export` is granted (#4969 partial grant)", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["audit_export"],
    });
    expect(buildAllowedNavSet(auth).has("/admin")).toBe(true);
  });

  it("shows /admin when only `roles_grants` is granted (#4969 partial grant)", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["roles_grants"],
    });
    expect(buildAllowedNavSet(auth).has("/admin")).toBe(true);
  });
});

describe("isPermissionCatalogEnforced", () => {
  it("is false for null auth (fail-open)", () => {
    expect(isPermissionCatalogEnforced(null)).toBe(false);
  });

  it("is false when all_scopes is true even with enforcement reported", () => {
    const auth = makeAuth({ all_scopes: true, permission_catalog_enforced: true });
    expect(isPermissionCatalogEnforced(auth)).toBe(false);
  });

  it("is false when the server does not report enforcement", () => {
    const auth = makeAuth({ all_scopes: false, permission_catalog_enforced: false });
    expect(isPermissionCatalogEnforced(auth)).toBe(false);
  });

  it("is true when catalog is enforced and all_scopes is false", () => {
    const auth = makeAuth({ all_scopes: false, permission_catalog_enforced: true });
    expect(isPermissionCatalogEnforced(auth)).toBe(true);
  });
});

describe("visibleAdminPanels (#4969)", () => {
  it("shows every panel when auth is absent (fail-open)", () => {
    const visible = visibleAdminPanels(null);
    expect(visible.has("invitations")).toBe(true);
    expect(visible.has("assignments")).toBe(true);
    expect(visible.has("roles")).toBe(true);
    expect(visible.has("identityAccess")).toBe(true);
    expect(visible.has("tokens")).toBe(true);
    expect(visible.has("audit")).toBe(true);
  });

  it("shows every panel when the catalog is not enforced (legacy fail-open)", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: false,
      allowed_permission_features: [],
    });
    expect(visibleAdminPanels(auth).size).toBe(6);
  });

  it("shows only the tokens panel for a `tokens`-only grant", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["tokens"],
    });
    const visible = visibleAdminPanels(auth);
    expect([...visible]).toEqual(["tokens"]);
  });

  it("shows only the audit panel for an `audit_export`-only grant", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["audit_export"],
    });
    const visible = visibleAdminPanels(auth);
    expect([...visible]).toEqual(["audit"]);
  });

  it("shows assignments and roles (not invitations/identityAccess) for a `roles_grants`-only grant", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["roles_grants"],
    });
    const visible = visibleAdminPanels(auth);
    expect(visible.has("assignments")).toBe(true);
    expect(visible.has("roles")).toBe(true);
    expect(visible.has("invitations")).toBe(false);
    expect(visible.has("identityAccess")).toBe(false);
    expect(visible.has("tokens")).toBe(false);
    expect(visible.has("audit")).toBe(false);
  });

  it("shows invitations and identityAccess (not assignments/roles) for an `identity_admin`-only grant", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["identity_admin"],
    });
    const visible = visibleAdminPanels(auth);
    expect(visible.has("invitations")).toBe(true);
    expect(visible.has("identityAccess")).toBe(true);
    expect(visible.has("assignments")).toBe(false);
    expect(visible.has("roles")).toBe(false);
  });

  it("shows no panels for a session with no admin-gating families", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["ask_search", "service_runtime"],
    });
    expect(visibleAdminPanels(auth).size).toBe(0);
  });
});

describe("canAccessAdminRoute (#4969)", () => {
  it("is true when auth is absent (fail-open)", () => {
    expect(canAccessAdminRoute(null)).toBe(true);
  });

  it("is true when the catalog is not enforced (legacy fail-open)", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: false,
      allowed_permission_features: [],
    });
    expect(canAccessAdminRoute(auth)).toBe(true);
  });

  it("is false when the catalog is enforced and no admin family is granted", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["ask_search"],
    });
    expect(canAccessAdminRoute(auth)).toBe(false);
  });

  it("is true for a partial `tokens`-only grant", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["tokens"],
    });
    expect(canAccessAdminRoute(auth)).toBe(true);
  });
});

describe("canAccessNav", () => {
  it("returns true for always-allowed route regardless of auth", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: [],
    });
    expect(canAccessNav("/dashboard", auth)).toBe(true);
    expect(canAccessNav("/status", auth)).toBe(true);
    expect(canAccessNav("/", auth)).toBe(true);
  });

  it("returns true for unknown route (fail-open)", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: [],
    });
    expect(canAccessNav("/some-unknown-route", auth)).toBe(true);
  });

  it("returns false for mapped route when family not granted", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["ask_search"],
    });
    expect(canAccessNav("/sbom", auth)).toBe(false);
  });

  it("returns true for mapped route when family is granted", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["supply_chain"],
    });
    expect(canAccessNav("/sbom", auth)).toBe(true);
  });

  it("returns false for /admin when identity_admin not granted (#3703)", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["ask_search"],
    });
    expect(canAccessNav("/admin", auth)).toBe(false);
  });
});
