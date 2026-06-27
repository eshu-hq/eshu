// pages/AdminPage.tsx
// Console admin UX (issue #3703, #3462 criterion #4): a capability-aware admin
// surface covering invitations, role assignments, roles & grants, IdP
// providers, IdP group mappings, API tokens, and audit. Each concern is its own
// panel component (under apps/console/src/pages/admin/) that loads its own data,
// renders metadata only (no secrets/hashes/invite-codes/external-group names),
// and surfaces "unavailable" on a load error rather than fabricated rows.
//
// This page is UX only — the server enforces authorization on every request.
// The /admin route and nav link are gated behind the identity_admin permission
// family in auth/capabilityAccess.ts (fail-open until #3684 persists features).
import type { EshuApiClient } from "../api/client";
import { AdminAssignmentsPanel } from "./admin/AdminAssignmentsPanel";
import { AdminAuditPanel } from "./admin/AdminAuditPanel";
import { AdminIdPGroupMappingsPanel } from "./admin/AdminIdPGroupMappingsPanel";
import { AdminInvitationsPanel } from "./admin/AdminInvitationsPanel";
import { AdminProvidersPanel } from "./admin/AdminProvidersPanel";
import { AdminRolesPanel } from "./admin/AdminRolesPanel";
import { AdminTokensPanel } from "./admin/AdminTokensPanel";
import "./liveInventory.css";
import "./adminPage.css";

export function AdminPage({
  client
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  return (
    <section className="page-shell">
      <h2>Admin</h2>
      <p className="admin-subtitle">
        Capability-aware admin UX. The server enforces authorization on every
        request; this surface renders metadata only and never exposes secrets.
      </p>
      <div className="panel-grid">
        <AdminInvitationsPanel client={client} />
        <AdminAssignmentsPanel client={client} />
        <AdminRolesPanel client={client} />
        <AdminProvidersPanel client={client} />
        <AdminIdPGroupMappingsPanel client={client} />
        <AdminTokensPanel client={client} />
        <AdminAuditPanel client={client} />
      </div>
    </section>
  );
}
