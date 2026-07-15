// pages/VulnerabilitiesPage.tsx
// Two views of vulnerability truth, kept under one nav item so the distinction
// is explicit: "Reachable" lists findings correlated to indexed services
// (impact findings), while "Catalog" browses the broader known
// vulnerability-intelligence catalog (GET /api/v0/supply-chain/advisories).
// Catalog rows are known intelligence only and do not imply service impact.
import { useState } from "react";

import { AdvisoryCatalog } from "./VulnerabilitiesCatalog";
import { ReachableAdvisories } from "./VulnerabilitiesReachable";
import type { EshuApiClient } from "../api/client";
import type { ConsoleModel } from "../console/types";

type Tab = "reachable" | "catalog";

export function VulnerabilitiesPage({
  model,
  client,
}: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [tab, setTab] = useState<Tab>("reachable");
  return (
    <div className="page">
      <div className="page-intro">
        <h2>Vulnerabilities</h2>
        <p>
          Separate views of vulnerability truth: findings <strong>reachable in our services</strong>{" "}
          versus the broader <strong>known intelligence</strong> catalog.
        </p>
      </div>
      <div className="seg" role="tablist" aria-label="Vulnerability views">
        <button
          role="tab"
          aria-selected={tab === "reachable"}
          className={tab === "reachable" ? "active" : ""}
          onClick={() => setTab("reachable")}
        >
          Reachable in services
        </button>
        <button
          role="tab"
          aria-selected={tab === "catalog"}
          className={tab === "catalog" ? "active" : ""}
          onClick={() => setTab("catalog")}
        >
          Known intelligence (catalog)
        </button>
      </div>
      {tab === "reachable" ? (
        <ReachableAdvisories model={model} />
      ) : (
        <AdvisoryCatalog model={model} client={client} />
      )}
    </div>
  );
}
