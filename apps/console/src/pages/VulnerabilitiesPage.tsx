// pages/VulnerabilitiesPage.tsx
// Two views of vulnerability truth, kept under one nav item so the distinction
// is explicit: "Reachable" lists findings correlated to indexed services
// (impact findings), while "Catalog" browses the broader known
// vulnerability-intelligence catalog (GET /api/v0/supply-chain/advisories).
// Catalog rows are known intelligence only and do not imply service impact.
import { useRef, useState } from "react";

import { AdvisoryCatalog } from "./VulnerabilitiesCatalog";
import { ReachableAdvisories } from "./VulnerabilitiesReachable";
import type { EshuApiClient } from "../api/client";
import type { ConsoleModel } from "../console/types";
import type { SectionProvenance } from "../console/types";
import "./vulnerabilitiesPage.css";

type Tab = "reachable" | "catalog";

interface ViewStatus {
  readonly count: number | null;
  readonly detail: string;
  readonly state: "empty" | "loading" | "populated" | "unavailable";
  readonly truncated?: boolean;
  readonly value: string;
}

export function VulnerabilitiesPage({
  model,
  client,
}: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [tab, setTab] = useState<Tab>("reachable");
  const reachableTab = useRef<HTMLButtonElement>(null);
  const catalogTab = useRef<HTMLButtonElement>(null);
  const reachableStatus = statusForReachable(model);
  const catalogStatus = statusForCatalog(model);

  function selectTab(next: Tab): void {
    setTab(next);
    (next === "reachable" ? reachableTab : catalogTab).current?.focus();
  }

  function handleTabKey(event: React.KeyboardEvent<HTMLButtonElement>): void {
    if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
      event.preventDefault();
      selectTab(tab === "reachable" ? "catalog" : "reachable");
    } else if (event.key === "Home" || event.key === "End") {
      event.preventDefault();
      selectTab(event.key === "Home" ? "reachable" : "catalog");
    }
  }

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Vulnerabilities</h2>
        <p>
          Separate views of vulnerability truth: findings <strong>reachable in our services</strong>{" "}
          versus the broader <strong>known intelligence</strong> catalog.
        </p>
      </div>
      <section className="vulnerability-view-status" aria-label="Vulnerability view status">
        <ViewStatusCard label="Reachable impact" status={reachableStatus} view="reachable" />
        <ViewStatusCard label="Known intelligence" status={catalogStatus} view="catalog" />
      </section>
      <div className="seg" role="tablist" aria-label="Vulnerability views">
        <button
          ref={reachableTab}
          id="vulnerability-tab-reachable"
          role="tab"
          aria-selected={tab === "reachable"}
          aria-controls="vulnerability-panel-reachable"
          className={tab === "reachable" ? "active" : ""}
          tabIndex={tab === "reachable" ? 0 : -1}
          onClick={() => setTab("reachable")}
          onKeyDown={handleTabKey}
        >
          Reachable in services
        </button>
        <button
          ref={catalogTab}
          id="vulnerability-tab-catalog"
          role="tab"
          aria-selected={tab === "catalog"}
          aria-controls="vulnerability-panel-catalog"
          className={tab === "catalog" ? "active" : ""}
          tabIndex={tab === "catalog" ? 0 : -1}
          onClick={() => setTab("catalog")}
          onKeyDown={handleTabKey}
        >
          Known intelligence (catalog)
        </button>
      </div>
      {tab === "reachable" ? (
        <div
          id="vulnerability-panel-reachable"
          role="tabpanel"
          aria-labelledby="vulnerability-tab-reachable"
          tabIndex={0}
        >
          <ReachableAdvisories model={model} />
        </div>
      ) : (
        <div
          id="vulnerability-panel-catalog"
          role="tabpanel"
          aria-labelledby="vulnerability-tab-catalog"
          tabIndex={0}
        >
          <AdvisoryCatalog model={model} client={client} />
        </div>
      )}
    </div>
  );
}

function ViewStatusCard({
  label,
  status,
  view,
}: {
  readonly label: string;
  readonly status: ViewStatus;
  readonly view: "catalog" | "reachable";
}): React.JSX.Element {
  return (
    <article
      className="vulnerability-view-status-card"
      data-count={status.count ?? ""}
      data-state={status.state}
      data-truncated={status.truncated === true ? "true" : "false"}
      data-vulnerability-view={view}
    >
      <span>{label}</span>
      <strong>{status.value}</strong>
      <small>{status.detail}</small>
    </article>
  );
}

function sectionState(
  model: ConsoleModel,
  key: "advisories" | "vulnerabilities",
): SectionProvenance {
  return model.provenance[key] ?? (model.source === "demo" ? "demo" : "loading");
}

function statusForReachable(model: ConsoleModel): ViewStatus {
  const state = sectionState(model, "vulnerabilities");
  if (state === "loading") {
    return {
      count: null,
      state: "loading",
      value: "Loading reachable impact",
      detail: "Impact findings are still loading.",
    };
  }
  if (state === "unavailable") {
    return {
      count: null,
      state: "unavailable",
      value: "Reachable impact unavailable",
      detail: "No zero-impact claim is shown while this read is unavailable.",
    };
  }
  if (model.vulnerabilities.length === 0) {
    return {
      count: 0,
      state: "empty",
      value: "0 affected services proven",
      detail: "No affected service is proven by impact findings.",
    };
  }
  const findingCount = model.vulnerabilities.length;
  const serviceBackedFindingCount = model.vulnerabilities.filter((row) =>
    row.services.some((service) => service.trim() !== ""),
  ).length;
  const noServiceFindingCount = findingCount - serviceBackedFindingCount;
  if (serviceBackedFindingCount === 0) {
    return {
      count: 0,
      state: "populated",
      value: "0 affected services proven",
      detail: `${findingCount} impact ${findingCount === 1 ? "finding has" : "findings have"} no admitted service evidence.`,
    };
  }
  return {
    count: serviceBackedFindingCount,
    state: "populated",
    value: `${serviceBackedFindingCount} service-backed ${serviceBackedFindingCount === 1 ? "finding" : "findings"}`,
    detail:
      noServiceFindingCount > 0
        ? `${findingCount} impact findings; ${noServiceFindingCount} ${noServiceFindingCount === 1 ? "has" : "have"} no admitted service evidence.`
        : `${findingCount} impact ${findingCount === 1 ? "finding has" : "findings have"} admitted service evidence.`,
  };
}

function statusForCatalog(model: ConsoleModel): ViewStatus {
  const state = sectionState(model, "advisories");
  if (state === "loading") {
    return {
      count: null,
      state: "loading",
      value: "Loading catalog intelligence",
      detail: "Known advisories are still loading independently.",
    };
  }
  if (state === "unavailable") {
    return {
      count: null,
      state: "unavailable",
      value: "Catalog intelligence unavailable",
      detail: "Reachable-impact state is unaffected.",
    };
  }
  const summary = model.advisoryCatalogSummary;
  if (state === "empty" || summary?.count === 0) {
    return {
      count: 0,
      state: "empty",
      value: "0 known advisories",
      detail: "The bounded catalog read returned no advisory intelligence.",
    };
  }
  if (summary === null) {
    return {
      count: null,
      state: "unavailable",
      value: "Catalog summary unavailable",
      detail: "No total is inferred from rendered rows.",
    };
  }
  return {
    count: summary.count,
    state: "populated",
    truncated: summary.truncated,
    value: `${summary.count}${summary.truncated ? "+" : ""} known advisories`,
    detail: summary.truncated
      ? "Bounded first page; more catalog entries are available. Catalog intelligence only; reachability is not implied."
      : "Catalog intelligence only; reachability is not implied.",
  };
}
