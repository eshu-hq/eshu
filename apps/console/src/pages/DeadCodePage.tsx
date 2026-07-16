import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";

import {
  classificationFromFinding,
  codeGraphHref,
  deadCodeLanguages,
  deadCodeScanLabel,
  groupDeadCodeByRepository,
  kindFromFinding,
  locationFromFinding,
  locFromFinding,
  matchesDeadCodeQuery,
  sourceHref,
  symbolFromFinding,
  uniqueStrings,
  type DeadCodeRepositoryGroup,
} from "./deadCodePresentation";
import type { EshuApiClient } from "../api/client";
import { loadDeadCodePage } from "../api/deadCode";
import type { DeadCodePage as LiveDeadCodePage } from "../api/deadCode";
import { Panel, StatTile, Badge, TruthChip } from "../components/atoms";
import type { ConsoleModel } from "../console/types";
import { fmt, uiTruth } from "../console/types";
import type { RepositoryCatalogState } from "../repositoryCatalogLifecycle";
import { DeadCodeRepositoryBreakdown, RepositoryCoverageTile } from "./DeadCodeRepositoryBreakdown";
import "./liveInventory.css";

const ANY = "all";
const LIVE_LIMIT = 100;
const DEAD_CODE_CANDIDATE_KINDS = [
  "Function",
  "Class",
  "Struct",
  "Interface",
  "Trait",
  "SqlFunction",
] as const;

interface DeadCodeFilters {
  readonly candidateKind: string;
  readonly language: string;
  readonly repoId: string;
}

interface DeadCodeLoad {
  readonly client: EshuApiClient;
  readonly filtersKey: string;
  readonly promise: Promise<LiveDeadCodePage>;
}

const EMPTY_FILTERS: DeadCodeFilters = { candidateKind: "", language: "", repoId: "" };

export function DeadCodePage({
  client,
  model,
  repositoryCatalog,
}: {
  readonly client?: EshuApiClient;
  readonly model: ConsoleModel;
  readonly repositoryCatalog?: RepositoryCatalogState;
}): React.JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const initialFilters = filtersFromSearchParams(searchParams);
  const [livePage, setLivePage] = useState<LiveDeadCodePage | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [draft, setDraft] = useState<DeadCodeFilters>(initialFilters);
  const [applied, setApplied] = useState<DeadCodeFilters>(initialFilters);
  const [showRepositoryBreakdown, setShowRepositoryBreakdown] = useState(false);
  const [classification, setClassification] = useState(ANY);
  const [kind, setKind] = useState(ANY);
  const [query, setQuery] = useState(() => searchParams.get("q") ?? "");
  const routeRepoId = searchParams.get("repo_id") ?? "";
  const routeLanguage = searchParams.get("language") ?? "";
  const loadRef = useRef<DeadCodeLoad | null>(null);

  useEffect(() => {
    setDraft((current) => withRouteScope(current, routeLanguage, routeRepoId));
    setApplied((current) => withRouteScope(current, routeLanguage, routeRepoId));
  }, [routeLanguage, routeRepoId]);

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setLivePage(null);
      setBusy(false);
      setErr("");
      return;
    }
    setBusy(true);
    setErr("");
    const filters = {
      candidateKind: applied.candidateKind || undefined,
      language: applied.language || undefined,
      limit: LIVE_LIMIT,
      repoId: applied.repoId || undefined,
    };
    const filtersKey = JSON.stringify(filters);
    const load =
      loadRef.current?.client === client && loadRef.current.filtersKey === filtersKey
        ? loadRef.current
        : {
            client,
            filtersKey,
            promise: loadDeadCodePage(client, filters),
          };
    loadRef.current = load;
    void load.promise
      .then((page) => {
        if (!cancelled) {
          setLivePage(page);
          setBusy(false);
        }
      })
      .catch((error) => {
        if (loadRef.current === load) loadRef.current = null;
        if (!cancelled) {
          setLivePage(null);
          setBusy(false);
          setErr(error instanceof Error ? error.message : "failed to load dead-code candidates");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [applied, client]);

  const all = (livePage?.rows ?? model.findings).filter((finding) => finding.type === "Dead code");
  const repositoryNames = useMemo(
    () => new Map(repositoryCatalog?.repositories.map((repo) => [repo.id, repo.name]) ?? []),
    [repositoryCatalog?.repositories],
  );
  const classifications = uniqueStrings(all.map(classificationFromFinding).filter(Boolean));
  const kinds = uniqueStrings([
    ...DEAD_CODE_CANDIDATE_KINDS.map((candidateKind) => candidateKind.toLowerCase()),
    ...all.map(kindFromFinding).filter(Boolean),
  ]);
  const filtered = all.filter(
    (finding) =>
      (classification === ANY || classificationFromFinding(finding) === classification) &&
      (kind === ANY || kindFromFinding(finding) === kind) &&
      matchesDeadCodeQuery(finding, query),
  );
  const grouped = groupDeadCodeByRepository(filtered, repositoryNames);
  const repositoryGroups = groupDeadCodeByRepository(all, repositoryNames);
  const languages = deadCodeLanguages(livePage, all);
  const totalLoc = all.reduce((sum, finding) => sum + locFromFinding(finding), 0);
  const highConfidence = all.filter(
    (finding) => finding.classification === "unused" || finding.truth === "exact",
  ).length;
  const source = client ? (busy ? "loading" : err ? "unavailable" : "live") : model.source;
  const scanLabel = deadCodeScanLabel(livePage, client !== undefined);

  function applyFilters(): void {
    const next = {
      candidateKind: applied.candidateKind,
      language: draft.language.trim(),
      repoId: draft.repoId.trim(),
    };
    setApplied(next);
    setSearchParams((current) => {
      const params = new URLSearchParams(current);
      setOrDelete(params, "repo_id", next.repoId);
      setOrDelete(params, "language", next.language);
      return params;
    });
  }

  function resetLiveFilters(): void {
    setDraft(EMPTY_FILTERS);
    setApplied(EMPTY_FILTERS);
    setKind(ANY);
    setSearchParams((current) => {
      const params = new URLSearchParams(current);
      params.delete("repo_id");
      params.delete("language");
      return params;
    });
  }

  function selectKind(value: string): void {
    setKind(value);
    const candidateKind =
      DEAD_CODE_CANDIDATE_KINDS.find((entry) => entry.toLowerCase() === value.toLowerCase()) ?? "";
    setApplied((current) => ({ ...current, candidateKind }));
  }

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Dead code</h2>
        <p>
          Graph-backed dead-code candidates from{" "}
          <span className="mono">POST /api/v0/code/dead-code</span>. Candidates are grouped by
          repository and stay empty when the API has no analyzer output. Select a location to open
          the source file.
        </p>
      </div>

      <div className="grid g-4">
        <StatTile
          label="Candidates shown"
          value={all.length}
          color="var(--ember)"
          sub="current bounded response"
        />
        <RepositoryCoverageTile
          count={repositoryGroups.length}
          expanded={showRepositoryBreakdown}
          onToggle={() => setShowRepositoryBreakdown((current) => !current)}
        />
        <StatTile
          label="Est. LOC shown"
          value={fmt(totalLoc)}
          color="var(--violet)"
          sub="current result window"
        />
        <StatTile
          label="High confidence shown"
          value={highConfidence}
          color="var(--teal)"
          sub={scanLabel}
        />
      </div>
      {showRepositoryBreakdown ? <DeadCodeRepositoryBreakdown groups={repositoryGroups} /> : null}
      <p className="dead-code-scan-status mt" role="status">
        {scanLabel}. All summary counts describe this returned result window, not the corpus.
      </p>

      <div className="repo-toolbar mt">
        <div className="searchbox repo-search">
          <input
            aria-label="Find dead-code candidate"
            placeholder="Find a symbol, file or repo..."
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
        {client ? (
          <>
            <input
              aria-label="Repository selector"
              className="popover-input mono"
              list="dead-code-repository-options"
              placeholder="Search repository name or identifier"
              value={draft.repoId}
              onChange={(event) =>
                setDraft((current) => ({ ...current, repoId: event.target.value }))
              }
            />
            <datalist id="dead-code-repository-options">
              {(repositoryCatalog?.repositories ?? []).map((repository) => (
                <option key={repository.id} value={repository.id}>
                  {repository.name} · {repository.id}
                </option>
              ))}
            </datalist>
            {repositoryCatalog?.kind === "unavailable" ? (
              <span className="t-mut" role="status">
                Repository choices unavailable; enter a canonical identifier.
              </span>
            ) : repositoryCatalog?.kind === "ready" &&
              repositoryCatalog.completeness === "truncated" ? (
              <span className="t-mut" role="status">
                Repository choices are incomplete: {repositoryCatalog.warning}
              </span>
            ) : null}
            <input
              aria-label="Language selector"
              className="popover-input mono"
              list="dead-code-language-options"
              placeholder="Search language"
              value={draft.language}
              onChange={(event) =>
                setDraft((current) => ({ ...current, language: event.target.value }))
              }
            />
            <datalist id="dead-code-language-options">
              {languages.map((language) => (
                <option key={language} value={language} />
              ))}
            </datalist>
            <button className="btn-ghost active" disabled={busy} onClick={applyFilters}>
              Apply
            </button>
            <button className="btn-ghost" disabled={busy} onClick={resetLiveFilters}>
              Reset
            </button>
          </>
        ) : null}
        <div className="seg" aria-label="Dead-code kind filter">
          {[ANY, ...kinds].map((value) => (
            <button
              aria-pressed={kind === value}
              key={value}
              className={kind === value ? "active" : ""}
              onClick={() => selectKind(value)}
            >
              {value === ANY
                ? "All kinds"
                : `${value} · ${all.filter((finding) => kindFromFinding(finding) === value).length}`}
            </button>
          ))}
        </div>
        <div className="seg" aria-label="Dead-code classification filter">
          {[ANY, ...classifications].map((value) => (
            <button
              aria-pressed={classification === value}
              key={value}
              className={classification === value ? "active" : ""}
              onClick={() => setClassification(value)}
            >
              {value === ANY ? "Any" : value}
            </button>
          ))}
        </div>
      </div>

      <div className="evidence-workbench mt" aria-label="Dead-code workbench">
        <Panel
          className="flush"
          title={`${filtered.length} candidates`}
          sub={`Grouped by repository · ${source}`}
        >
          <div className="table-scroll">
            <table className="tbl wide">
              <thead>
                <tr>
                  <th>Symbol</th>
                  <th>Kind</th>
                  <th>Language</th>
                  <th>Location</th>
                  <th>Refs</th>
                  <th>LOC</th>
                  <th>Confidence</th>
                  <th>Why dead</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {grouped.map((group) => (
                  <DeadCodeGroup key={group.key} group={group} />
                ))}
                {filtered.length === 0 ? (
                  <tr>
                    <td colSpan={9} className="empty">
                      {err
                        ? `Failed to load: ${err}`
                        : busy
                          ? "Loading dead-code candidates..."
                          : "No dead-code candidates from this source."}
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </Panel>
      </div>
    </div>
  );
}

function withRouteScope(
  current: DeadCodeFilters,
  language: string,
  repoId: string,
): DeadCodeFilters {
  if (current.language === language && current.repoId === repoId) return current;
  return { ...current, language, repoId };
}

function DeadCodeGroup({ group }: { readonly group: DeadCodeRepositoryGroup }): React.JSX.Element {
  const loc = group.rows.reduce((sum, finding) => sum + locFromFinding(finding), 0);
  return (
    <>
      <tr className="group-row">
        <td colSpan={9}>
          <span className="group-label" style={{ color: "var(--ember)" }}>
            {group.repository}
          </span>
          <span className="group-meta">
            {group.rows.length} dead · {fmt(loc)} LOC
          </span>
        </td>
      </tr>
      {group.rows.map((finding) => {
        const href = sourceHref(finding);
        return (
          <tr key={finding.id} className="cloud-row">
            <td className="cell-stack">
              <span className="mono" style={{ color: "var(--bone)", fontWeight: 600 }}>
                {symbolFromFinding(finding)}
              </span>
              <small>{finding.title}</small>
            </td>
            <td>
              <Badge tone="neutral">{kindFromFinding(finding)}</Badge>
            </td>
            <td className="dead-code-row-language">{finding.language ?? "language unavailable"}</td>
            <td className="t-mut mono" style={{ fontSize: ".74rem" }}>
              {href ? (
                <Link className="mono" to={href}>
                  {locationFromFinding(finding)}
                </Link>
              ) : (
                locationFromFinding(finding)
              )}
            </td>
            <td>
              <span className="mono" style={{ color: "var(--crit)", fontWeight: 700 }}>
                0
              </span>
            </td>
            <td className="t-mut mono" style={{ fontSize: ".78rem" }}>
              {locFromFinding(finding) || "—"}
            </td>
            <td>
              <TruthChip level={uiTruth(finding.truth)} />
            </td>
            <td className="t-mut" style={{ fontSize: ".78rem", maxWidth: 360 }}>
              {classificationFromFinding(finding) || "candidate"}
            </td>
            <td>
              <Link className="btn-ghost" to={codeGraphHref(finding)}>
                Open graph
              </Link>
            </td>
          </tr>
        );
      })}
    </>
  );
}

function filtersFromSearchParams(params: URLSearchParams): DeadCodeFilters {
  return {
    candidateKind: "",
    language: params.get("language")?.trim() ?? "",
    repoId: params.get("repo_id")?.trim() ?? "",
  };
}

function setOrDelete(params: URLSearchParams, name: string, value: string): void {
  if (value === "") params.delete(name);
  else params.set(name, value);
}
