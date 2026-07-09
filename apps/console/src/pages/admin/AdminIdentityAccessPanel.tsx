// pages/admin/AdminIdentityAccessPanel.tsx
// Admin -> Identity & Access (#4967, epic #4962 Wave 2): a tabbed area
// composing Providers (AdminProvidersPanel, full CRUD against the #4966 API),
// Group -> role mappings (AdminIdPGroupMappingsPanel, moved in unchanged), and
// a Sign-in policy placeholder tab (the real policy surface ships in #4968 —
// E-6). This component is UX only; the server enforces authorization on every
// request each panel makes.
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
      <div className="tab-strip" role="tablist" aria-label="Identity & Access sections">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            className={`tab-btn${tab === t.id ? " active" : ""}`}
            onClick={() => setTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div className="tab-panel" role="tabpanel">
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
