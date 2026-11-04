// pages/VulnerabilitiesReachableWorkspace.tsx
// Body of the "Reachable in services" tab: the finding list, or the grouped
// "By service" inventory. A group link from the inventory lands here as a
// service_id / workload_id / environment query parameter, which narrows the
// finding list to that group.
import { useState } from "react";
import { Link, useSearchParams } from "react-router";

import { ReachableAdvisories } from "./VulnerabilitiesReachable";
import { VulnerabilitiesByGroup } from "./VulnerabilitiesByGroup";
import type { EshuApiClient } from "../api/client";
import type { ImpactListFilter } from "../api/eshuConsoleVulnerabilities";
import type { ConsoleModel } from "../console/types";

type View = "findings" | "by-group";

const FILTER_KEYS = ["service_id", "workload_id", "environment"] as const;

function filterFromParams(params: URLSearchParams): ImpactListFilter | null {
  for (const key of FILTER_KEYS) {
    const value = params.get(key)?.trim();
    if (value) {
      return params.get("profile") === "comprehensive"
        ? { key, value, profile: "comprehensive" }
        : { key, value };
    }
  }
  return null;
}

export function ReachableWorkspace({
  model,
  client,
}: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [params] = useSearchParams();
  const filter = filterFromParams(params);
  // The picked view belongs to the filter it was picked under. A group link
  // changes the filter, so the workspace reopens the finding list for it
  // instead of keeping the grouped view on screen with a stale URL.
  const filterKey = filter === null ? "" : `${filter.key}=${filter.value}|${filter.profile ?? ""}`;
  const [picked, setPicked] = useState<{ readonly view: View; readonly filterKey: string }>({
    view: "findings",
    filterKey,
  });
  const view: View = picked.filterKey === filterKey ? picked.view : "findings";
  const setView = (next: View): void => setPicked({ view: next, filterKey });
  return (
    <div>
      <div className="seg" role="group" aria-label="Reachable view">
        <button
          type="button"
          aria-pressed={view === "findings"}
          className={view === "findings" ? "active" : ""}
          onClick={() => setView("findings")}
        >
          Finding list
        </button>
        <button
          type="button"
          aria-pressed={view === "by-group"}
          className={view === "by-group" ? "active" : ""}
          onClick={() => setView("by-group")}
        >
          By service
        </button>
      </div>
      {view === "by-group" ? (
        <VulnerabilitiesByGroup client={client} />
      ) : (
        <>
          {filter ? (
            <p role="status">
              {`Filtered to ${filter.key}: ${filter.value}.`}{" "}
              <Link to="/vulnerabilities">Clear filter</Link>
            </p>
          ) : null}
          <ReachableAdvisories model={model} client={client} filter={filter} />
        </>
      )}
    </div>
  );
}
