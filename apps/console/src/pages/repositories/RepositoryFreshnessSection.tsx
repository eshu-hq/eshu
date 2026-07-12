// pages/repositories/RepositoryFreshnessSection.tsx
// Fuller freshness surface for the repo source detail page (issue #5143):
// verdict chip, stage checklist, outstanding-by-stage counts, and the
// cross-repo shared-enrichment note. Lazy-loaded from RepoSourcePage.tsx
// (mirrors OperationsLiveBoard.tsx, issue #5137) so its code and the
// repositoryFreshness adapter ship in their own chunk instead of growing the
// eagerly loaded main bundle.
//
// Polls GET /api/v0/repositories/{id}/freshness only while the verdict is
// "building" or "unobserved" -- the states where the answer is expected to
// change soon. Polling stops once the repository reaches "current",
// "behind", or "unknown", or when the read degrades to unavailable, so an
// idle or broken repo does not keep the network busy.
import { useEffect, useState } from "react";

import type { EshuApiClient } from "../../api/client";
import {
  loadRepositoryFreshness,
  type RepositoryFreshness,
  type RepositoryFreshnessStages,
} from "../../api/repositoryFreshness";
import { Badge, Panel } from "../../components/atoms";
import { FreshnessChip } from "../../components/FreshnessChip";

const defaultPollMs = 12000;

const STAGE_ROWS: readonly {
  readonly key: keyof RepositoryFreshnessStages;
  readonly label: string;
}[] = [
  { key: "collected", label: "Collected" },
  { key: "reduced", label: "Reduced" },
  { key: "projected", label: "Projected" },
  { key: "materialized", label: "Materialized" },
];

// shouldKeepPolling reports whether the pipeline is still actively catching
// up (building) or hasn't started on a known push yet (unobserved) --
// "current", "behind", and "unknown" are stable answers that only change on
// the next push/webhook, so re-polling them on a timer would waste cycles for
// no visible change.
function shouldKeepPolling(freshness: RepositoryFreshness | null): boolean {
  if (freshness === null || freshness.provenance === "unavailable") return false;
  return freshness.verdict === "building" || freshness.verdict === "unobserved";
}

export function RepositoryFreshnessSection({
  client,
  repoId,
  pollMs = defaultPollMs,
}: {
  readonly client?: EshuApiClient;
  readonly repoId: string;
  readonly pollMs?: number;
}): React.JSX.Element | null {
  const [freshness, setFreshness] = useState<RepositoryFreshness | null>(null);

  useEffect(() => {
    setFreshness(null);
    if (!client || repoId === "") return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const refresh = (): void => {
      void loadRepositoryFreshness(client, repoId).then((next) => {
        if (cancelled) return;
        setFreshness(next);
        if (shouldKeepPolling(next)) {
          timer = setTimeout(refresh, pollMs > 0 ? pollMs : defaultPollMs);
        }
      });
    };
    refresh();
    return () => {
      cancelled = true;
      if (timer !== undefined) clearTimeout(timer);
    };
  }, [client, repoId, pollMs]);

  if (!client || repoId === "") return null;

  if (freshness === null) {
    return (
      <Panel className="mt" title="Freshness" sub="GET /api/v0/repositories/{id}/freshness">
        <p className="empty">Loading freshness…</p>
      </Panel>
    );
  }

  if (freshness.provenance === "unavailable") {
    return (
      <Panel className="mt" title="Freshness">
        <p className="empty">Freshness unavailable from this source.</p>
      </Panel>
    );
  }

  return (
    <Panel
      className="mt"
      title="Freshness"
      sub="GET /api/v0/repositories/{id}/freshness"
      action={<FreshnessChip freshness={freshness} />}
    >
      <p className="t-mut">{freshness.copy.detail}</p>
      <div className="section-label" style={{ marginTop: 12 }}>
        Stages
      </div>
      <ul className="plain-list">
        {STAGE_ROWS.map(({ key, label }) => (
          <li key={key}>
            <Badge tone={freshness.stages[key] ? "teal" : "neutral"} dot>
              {freshness.stages[key] ? "done" : "pending"}
            </Badge>{" "}
            {label}
          </li>
        ))}
      </ul>
      {freshness.outstandingByStage.length > 0 ? (
        <>
          <div className="section-label" style={{ marginTop: 12 }}>
            Outstanding work
          </div>
          <ul className="plain-list">
            {freshness.outstandingByStage.map((row) => (
              <li key={`${row.stage}:${row.status}`} className="mono">
                {row.stage} · {row.status}: {row.count}
              </li>
            ))}
          </ul>
        </>
      ) : null}
      {freshness.sharedEnrichment.pending ? (
        <p className="t-mut" style={{ marginTop: 12 }}>
          Cross-repo enrichment still running
          {freshness.sharedEnrichment.pendingDomains.length > 0
            ? `: ${freshness.sharedEnrichment.pendingDomains
                .map((domain) => `${domain.domain} (${domain.count})`)
                .join(", ")}`
            : ""}
        </p>
      ) : null}
    </Panel>
  );
}

// Default export lets RepoSourcePage.tsx's React.lazy() import the module
// directly (`lazy(() => import(...))`) instead of a `.then((m) => ({default:
// m.X}))` mapper, trimming a few bytes from the eagerly loaded caller under
// the console's tight main-bundle budget (scripts/console-bundle-budget.mjs).
export default RepositoryFreshnessSection;
