// pages/repositories/RepositoryFreshnessChip.tsx
// One-shot, on-demand freshness indicator for the Repositories page's
// selected-row detail panel (issue #5143). Lazy-loaded from
// RepositoriesPage.tsx (mirrors OperationsLiveBoard.tsx, issue #5137) so its
// code and the repositoryFreshness adapter ship in their own chunk instead of
// growing the eagerly loaded main bundle.
//
// Fetches exactly once per selected repository -- selecting a row is already
// an explicit, bounded, on-demand action (RepositoriesPage.tsx), so this never
// fires the freshness read for every row on page load. No polling here: the
// fuller, polling freshness surface lives on the repo source detail page
// (see RepositoryFreshnessSection.tsx).
import { useEffect, useState } from "react";

import type { EshuApiClient } from "../../api/client";
import { loadRepositoryFreshness, type RepositoryFreshness } from "../../api/repositoryFreshness";
import { FreshnessChip } from "../../components/FreshnessChip";

export function RepositoryFreshnessRow({
  client,
  repoId,
}: {
  readonly client?: EshuApiClient;
  readonly repoId: string;
}): React.JSX.Element | null {
  const [freshness, setFreshness] = useState<RepositoryFreshness | null>(null);

  useEffect(() => {
    let cancelled = false;
    setFreshness(null);
    if (!client || repoId === "") return;
    void loadRepositoryFreshness(client, repoId).then((next) => {
      if (!cancelled) setFreshness(next);
    });
    return () => {
      cancelled = true;
    };
  }, [client, repoId]);

  // Graceful degrade: an unavailable read hides the chip entirely rather than
  // showing a broken or fabricated badge (issue #5143 acceptance criteria).
  if (freshness === null || freshness.provenance === "unavailable") return null;
  return <FreshnessChip freshness={freshness} />;
}

// Default export lets RepositoriesPage.tsx's React.lazy() import the module
// directly (`lazy(() => import(...))`) instead of a `.then((m) => ({default:
// m.X}))` mapper, trimming a few bytes from the eagerly loaded caller under
// the console's tight main-bundle budget (scripts/console-bundle-budget.mjs).
export default RepositoryFreshnessRow;
