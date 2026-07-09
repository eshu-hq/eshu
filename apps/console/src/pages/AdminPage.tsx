// pages/AdminPage.tsx
// Console admin UX (issue #3703, #3462 criterion #4): a capability-aware admin
// surface covering invitations, role assignments, roles & grants, Identity &
// Access (providers, group mappings, sign-in policy — #4967), API tokens, and
// audit. Each concern is its own panel component (under
// apps/console/src/pages/admin/) that loads its own data, renders metadata
// only (no secrets/hashes/invite-codes/external-group names), and surfaces
// "unavailable" on a load error rather than fabricated rows.
//
// This page is UX only — the server enforces authorization on every request.
// The /admin route is gated by AdminRouteGuard (auth/AdminRouteGuard.tsx);
// each panel below is additionally gated by its own permission family via
// visibleAdminPanels (auth/capabilityAccess.ts) so a delegated role — e.g.
// one holding only `tokens` — sees just its panel (issue #4969). Fail-open
// (every panel) whenever the catalog is not enforced, preserving the
// pre-#4969 #3703 contract.
//
// AdminIdentityAccessPanel (the #4967 provider-config CRUD area — the
// heaviest addition on this page) is loaded via React.lazy/dynamic import to
// keep it out of the eager main chunk, the same pattern appRoutes.tsx already
// uses for WorkspacePage, needed to stay under the console bundle-budget gate
// (scripts/console-bundle-budget.mjs).
import { lazy, Suspense } from "react";

import type { BrowserSessionAuth, EshuApiClient } from "../api/client";
import { visibleAdminPanels } from "../auth/capabilityAccess";
import { AdminAssignmentsPanel } from "./admin/AdminAssignmentsPanel";
import { AdminAuditPanel } from "./admin/AdminAuditPanel";
import { AdminInvitationsPanel } from "./admin/AdminInvitationsPanel";
import { AdminRolesPanel } from "./admin/AdminRolesPanel";
import { AdminTokensPanel } from "./admin/AdminTokensPanel";
import "./liveInventory.css";
import "./adminPage.css";

const AdminIdentityAccessPanel = lazy(() =>
  import("./admin/AdminIdentityAccessPanel").then((m) => ({ default: m.AdminIdentityAccessPanel })),
);

export function AdminPage({
  client,
  baseUrl,
  auth,
}: {
  readonly client?: EshuApiClient;
  // baseUrl is the Eshu API origin, used to render the read-only OIDC
  // redirect URI / SAML SP entity id + ACS URL an operator copies into their
  // IdP (#4967). Defaults to "" (relative URLs) when not supplied.
  readonly baseUrl?: string;
  // auth drives per-panel family gating (#4969). Undefined fails open
  // (every panel renders), matching capabilityAccess.ts's ambiguous-case
  // policy.
  readonly auth?: BrowserSessionAuth | null;
}): React.JSX.Element {
  const visible = visibleAdminPanels(auth);
  return (
    <section className="page-shell">
      <h1>Admin</h1>
      <p className="admin-subtitle">
        Capability-aware admin UX. The server enforces authorization on every request; this surface
        renders metadata only and never exposes secrets.
      </p>
      <div className="panel-grid">
        {visible.has("invitations") ? <AdminInvitationsPanel client={client} /> : null}
        {visible.has("assignments") ? <AdminAssignmentsPanel client={client} /> : null}
        {visible.has("roles") ? <AdminRolesPanel client={client} /> : null}
        {visible.has("identityAccess") ? (
          <Suspense
            fallback={
              <section className="panel identity-access-panel">
                <p className="empty-note">Loading Identity & Access…</p>
              </section>
            }
          >
            <AdminIdentityAccessPanel client={client} baseUrl={baseUrl} />
          </Suspense>
        ) : null}
        {visible.has("tokens") ? <AdminTokensPanel client={client} /> : null}
        {visible.has("audit") ? <AdminAuditPanel client={client} /> : null}
      </div>
    </section>
  );
}
