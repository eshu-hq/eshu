// auth/AdminRouteGuard.test.tsx — TDD tests for the /admin route guard
// (issue #4969). Covers the four acceptance cases: no access under an
// enforced catalog, full access, partial-family access, and fail-open when
// the server does not report catalog enforcement.
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { AdminRouteGuard } from "./AdminRouteGuard";
import type { BrowserSessionAuth } from "../api/client";

function makeAuth(overrides: Partial<BrowserSessionAuth> = {}): BrowserSessionAuth {
  return {
    mode: "browser_session",
    all_scopes: true,
    ...overrides,
  };
}

function renderGuard(auth: BrowserSessionAuth | null | undefined): void {
  render(
    <MemoryRouter>
      <AdminRouteGuard auth={auth}>
        <div>admin page content</div>
      </AdminRouteGuard>
    </MemoryRouter>,
  );
}

describe("AdminRouteGuard (#4969)", () => {
  it("renders the 403 access screen when the catalog is enforced and no admin family is granted", () => {
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["ask_search"],
    });
    renderGuard(auth);
    expect(
      screen.getByRole("heading", { name: "You don't have access to this area" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("admin page content")).not.toBeInTheDocument();
    expect(warnSpy).toHaveBeenCalled();
    warnSpy.mockRestore();
  });

  it("renders children unchanged for a full admin session (all_scopes)", () => {
    renderGuard(makeAuth({ all_scopes: true }));
    expect(screen.getByText("admin page content")).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "You don't have access to this area" }),
    ).not.toBeInTheDocument();
  });

  it("renders children for a partial `tokens`-only grant (acceptance case (c))", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["tokens"],
    });
    renderGuard(auth);
    expect(screen.getByText("admin page content")).toBeInTheDocument();
  });

  it("fails open (renders children) when the server does not report catalog enforcement", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: false,
      allowed_permission_features: [],
    });
    renderGuard(auth);
    expect(screen.getByText("admin page content")).toBeInTheDocument();
  });

  it("fails open (renders children) when auth is absent", () => {
    renderGuard(null);
    expect(screen.getByText("admin page content")).toBeInTheDocument();
  });

  it("links to /profile from the 403 screen", () => {
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: [],
    });
    renderGuard(auth);
    expect(screen.getByRole("link", { name: "your effective permissions" })).toHaveAttribute(
      "href",
      "/profile",
    );
    warnSpy.mockRestore();
  });
});
