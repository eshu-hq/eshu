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
import { useEffect, useRef, useState, type FormEvent } from "react";

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
  // checking is true while a fetch triggered by this effect (initial load,
  // Check/Clear submit, or a repoId/client/pollMs change) is in flight. It is
  // intentionally separate from `freshness === null`: a same-scope refetch
  // must not blank prior freshness back to the "Loading freshness…" panel,
  // since that unmounts the form and drops focus from the input mid-submit.
  const [checking, setChecking] = useState(false);
  // expectedCommitInput is the raw, uncommitted text in the field.
  // appliedExpectedCommit is the value that actually drives the fetch --
  // it only changes on submit or clear, so typing does not refetch on every
  // keystroke (issue #5173).
  const [expectedCommitInput, setExpectedCommitInput] = useState("");
  const [appliedExpectedCommit, setAppliedExpectedCommit] = useState("");
  // submitNonce forces a refetch on every explicit Check click, including a
  // re-submit of the same SHA. Re-checking an unchanged SHA is the feature's
  // primary operator loop (confirm a push finally landed); keying the fetch
  // effect only on appliedExpectedCommit would make React bail out on an
  // identical value and silently drop the second click.
  const [submitNonce, setSubmitNonce] = useState(0);
  // RepoSourcePage keeps this section mounted across in-app navigation
  // between repos (repoId is a route param, not a remount trigger), so a SHA
  // typed for one repo must never silently drive the fetch for another. This
  // is React's documented "adjust state while rendering" pattern: comparing
  // the prop against a tracked previous value and resetting synchronously
  // avoids both a stale render and a wasted fetch with the old repo's SHA.
  const [expectedCommitRepoId, setExpectedCommitRepoId] = useState(repoId);
  if (repoId !== expectedCommitRepoId) {
    setExpectedCommitRepoId(repoId);
    setExpectedCommitInput("");
    setAppliedExpectedCommit("");
  }

  // scopeRef tracks the (client, repoId) pair the currently rendered
  // freshness belongs to. Only a scope change should blank the panel back to
  // "Loading freshness…" -- a refetch within the same scope (Check/Clear
  // submit, poll tick, pollMs change) keeps the prior freshness and form on
  // screen with `checking` as an inline indicator instead.
  const scopeRef = useRef<{ client: EshuApiClient | undefined; repoId: string }>({
    client: undefined,
    repoId: "",
  });

  useEffect(() => {
    const scopeChanged = scopeRef.current.client !== client || scopeRef.current.repoId !== repoId;
    scopeRef.current = { client, repoId };
    if (scopeChanged) {
      setFreshness(null);
    }
    if (!client || repoId === "") {
      setChecking(false);
      return;
    }
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    setChecking(true);

    const refresh = (): void => {
      void loadRepositoryFreshness(client, repoId, {
        expectedCommit: appliedExpectedCommit,
      }).then((next) => {
        if (cancelled) return;
        setFreshness(next);
        setChecking(false);
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
  }, [client, repoId, pollMs, appliedExpectedCommit, submitNonce]);

  if (!client || repoId === "") return null;

  function submitExpectedCommit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    setAppliedExpectedCommit(expectedCommitInput.trim());
    setSubmitNonce((nonce) => nonce + 1);
  }

  function clearExpectedCommit(): void {
    setExpectedCommitInput("");
    setAppliedExpectedCommit("");
  }

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
      <form
        onSubmit={submitExpectedCommit}
        style={{ display: "flex", alignItems: "center", gap: 8 }}
      >
        <label style={{ display: "flex", alignItems: "center", gap: 6, flex: 1 }}>
          <span className="t-mut">Expected commit</span>
          <input
            className="popover-input mono"
            onChange={(event) => setExpectedCommitInput(event.target.value)}
            placeholder="full or short SHA"
            value={expectedCommitInput}
          />
        </label>
        <button className="btn-ghost" disabled={checking} type="submit">
          Check
        </button>
        {appliedExpectedCommit !== "" ? (
          <button className="btn-ghost" onClick={clearExpectedCommit} type="button">
            Clear
          </button>
        ) : null}
        {checking ? (
          <span className="t-mut" style={{ fontSize: "0.76rem" }}>
            Checking…
          </span>
        ) : null}
      </form>
      <p className="t-mut" style={{ marginTop: 12 }}>
        {freshness.copy.detail}
      </p>
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
