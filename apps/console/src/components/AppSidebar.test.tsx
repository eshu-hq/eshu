// components/AppSidebar.test.tsx — component render test asserting the
// sidebar omits the /admin NavLink for a non-admin identity and includes it
// for an admin identity (issue #4996). The underlying gating logic
// (buildAllowedNavSet / canAccessNav("/admin")) is already unit-tested in
// ../auth/capabilityAccess.test.ts; this closes the component-level gap by
// proving AppSidebar actually honors the allowedNav set it is given.
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { AppSidebar } from "./AppSidebar";
import type { SourceState } from "./SourceControls";
import type { BrowserSessionAuth } from "../api/client";
import { buildAllowedNavSet } from "../auth/capabilityAccess";
import { demoModel } from "../console/demoModel";
import { ConsoleI18nProvider } from "../i18n/provider";

function makeAuth(overrides: Partial<BrowserSessionAuth> = {}): BrowserSessionAuth {
  return {
    mode: "browser_session",
    all_scopes: true,
    ...overrides,
  };
}

const sourceFixture: SourceState = {
  base: "/eshu-api/",
  key: "demo",
  mode: "demo",
  status: "connected",
  msg: "",
};

function renderSidebar(auth: BrowserSessionAuth | null | undefined): void {
  render(
    <MemoryRouter>
      <ConsoleI18nProvider>
        <AppSidebar
          allowedNav={buildAllowedNavSet(auth)}
          visibleModel={demoModel}
          model={demoModel}
          source={sourceFixture}
          backendMode="demo"
        />
      </ConsoleI18nProvider>
    </MemoryRouter>,
  );
}

describe("AppSidebar nav gating (#4996)", () => {
  it("omits the /admin NavLink for a non-admin identity", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["ask_search"],
    });
    renderSidebar(auth);
    expect(screen.queryByRole("link", { name: "Admin" })).not.toBeInTheDocument();
  });

  it("includes the /admin NavLink for an admin identity", () => {
    const auth = makeAuth({
      all_scopes: false,
      permission_catalog_enforced: true,
      allowed_permission_features: ["identity_admin"],
    });
    renderSidebar(auth);
    expect(screen.getByRole("link", { name: "Admin" })).toHaveAttribute("href", "/admin");
  });
});
