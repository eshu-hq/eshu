// pages/admin/AdminIdentityAccessPanel.tsx
// Admin -> Identity & Access (#4967, epic #4962 Wave 2): a tabbed area
// composing Providers (AdminProvidersPanel, full CRUD against the #4966 API),
// Group -> role mappings (AdminIdPGroupMappingsPanel, moved in unchanged), and
// a Sign-in policy placeholder tab (the real policy surface ships in #4968 —
// E-6). This component is UX only; the server enforces authorization on every
// request each panel makes.
//
// The tab strip reuses the console's real segmented-control primitive (.seg,
// styles.css) with the exact role="tablist"/role="tab"/aria-selected markup
// VulnerabilitiesPage.tsx already uses for its Reachable/Catalog tabs — not a
// bespoke tab-strip style.
import { useState } from "react";

import { AdminIdPGroupMappingsPanel } from "./AdminIdPGroupMappingsPanel";
import { AdminProvidersPanel } from "./AdminProvidersPanel";
import type { EshuApiClient } from "../../api/client";
import { Panel } from "../../components/atoms";

type IdentityAccessTab = "providers" | "group-mappings" | "sign-in-policy";

const TABS: readonly { readonly id: IdentityAccessTab; readonly label: string }[] = [
  { id: "providers", label: "Providers" },
  { id: "group-mappings", label: "Group → role mappings" },
  { id: "sign-in-policy", label: "Sign-in policy" },
];

// tabId/panelId complete the ARIA tabs pattern (WAI-ARIA APG): each tab
// references the panel it controls via aria-controls, and the panel
// references its controlling tab via aria-labelledby. Only one tab panel is
// ever mounted at a time (conditional rendering below), so a single wrapper
// element whose id/aria-labelledby follow the active tab is sufficient —
// there is never more than one id in the DOM at once.
function tabId(t: IdentityAccessTab): string {
  return `identity-access-tab-${t}`;
}

function panelId(t: IdentityAccessTab): string {
  return `identity-access-panel-${t}`;
}

export function AdminIdentityAccessPanel({
  client,
  baseUrl,
}: {
  readonly client?: EshuApiClient;
  readonly baseUrl?: string;
}): React.JSX.Element {
  const [tab, setTab] = useState<IdentityAccessTab>("providers");

  return (
    <Panel title="Identity & Access" className="identity-access-panel">
      <div className="seg" role="tablist" aria-label="Identity & Access sections">
        {TABS.map((t) => (
          <button
            key={t.id}
            id={tabId(t.id)}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            aria-controls={panelId(t.id)}
            className={tab === t.id ? "active" : ""}
            onClick={() => setTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div
        id={panelId(tab)}
        className="identity-access-tab-panel"
        role="tabpanel"
        aria-labelledby={tabId(tab)}
      >
        {tab === "providers" ? <AdminProvidersPanel client={client} baseUrl={baseUrl} /> : null}
        {tab === "group-mappings" ? <AdminIdPGroupMappingsPanel client={client} /> : null}
        {tab === "sign-in-policy" ? (
          <Panel title="Sign-in policy">
            <p className="empty-note">
              Sign-in policy (session lifetime, MFA requirements, allowed sign-in methods) ships in
              a follow-up (#4968). Nothing to configure here yet.
            </p>
          </Panel>
        ) : null}
      </div>
    </Panel>
  );
}
