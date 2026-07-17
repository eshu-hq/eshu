// pages/SemanticSearchPage.tsx
// Real semantic-search console surface: fronts POST /api/v0/search/semantic
// (see api/semanticSearch.ts). A query runs against the persisted curated
// search-document index; a language facet (chips + counts, from the response's
// facets.languages) narrows the corpus and round-trips through the URL so a
// shared link restores the same view. No fabricated data: loading, empty, and
// error states are rendered honestly.
//
// source_tool scope note (issue #4024): source_tool is an EDGE property (who
// resolved a relationship), not a property of a search document, so this page
// intentionally does not add a source_tool facet. That breakdown already lives
// on the Relationships view (see pages/RelationshipsPage.tsx's ToolFilter /
// SourceToolBreakdown, wired from relationshipsCatalog.ts's per-verb
// `sourceTools`) — the correct home for edge-provenance faceting, not this
// document-search page.
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router-dom";

import { SemanticRepositorySelector } from "./SemanticRepositorySelector";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";
import {
  searchSemantic,
  type SemanticSearchResponse,
  type SemanticSearchResult,
} from "../api/semanticSearch";
import { Panel, TruthChip, FreshDot } from "../components/atoms";
import { uiFresh, uiTruth } from "../console/types";
import {
  loadingRepositoryCatalog,
  type RepositoryCatalogState,
} from "../repositoryCatalogLifecycle";
import "./semanticSearchPage.css";

interface FormState {
  readonly repoId: string;
  readonly query: string;
}

type SearchState =
  | { readonly status: "idle" }
  | { readonly status: "loading" }
  | { readonly status: "error"; readonly message: string }
  | { readonly status: "ready"; readonly data: SemanticSearchResponse };

export function SemanticSearchPage({
  client,
  repositoryCatalog = loadingRepositoryCatalog,
}: {
  readonly client?: EshuApiClient;
  readonly repositoryCatalog?: RepositoryCatalogState;
}): React.JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const request = useMemo(() => requestFromSearch(searchParams), [searchParams]);
  const repositoryResolution = useMemo(
    () => resolveRepository(repositoryCatalog, request.repoId),
    [repositoryCatalog, request.repoId],
  );
  const resolvedRepository =
    repositoryResolution.status === "resolved" ? repositoryResolution.repository : undefined;
  const [form, setForm] = useState<FormState>({
    repoId: resolvedRepository?.id ?? "",
    query: request.query,
  });
  const [result, setResult] = useState<SearchState>({ status: "idle" });
  const hasLiveClient = client !== undefined;
  const isBounded = resolvedRepository !== undefined && request.query.trim().length > 0;

  // latestLoad sequences concurrent searches: rapid facet toggles or query
  // resubmits fire overlapping requests, and responses can arrive out of order.
  // Only the newest request is allowed to commit its result, so a slow earlier
  // response can never overwrite a newer one with stale (wrong-facet) results.
  const latestLoad = useRef(0);
  const load = useCallback(
    async (next: { repoId: string; query: string; languages: readonly string[] }) => {
      if (!client) return;
      const seq = ++latestLoad.current;
      setResult({ status: "loading" });
      try {
        const data = await searchSemantic(client, {
          repoId: next.repoId,
          query: next.query,
          languages: next.languages,
        });
        if (seq === latestLoad.current) setResult({ status: "ready", data });
      } catch (error) {
        if (seq === latestLoad.current) {
          setResult({
            status: "error",
            message: error instanceof Error ? error.message : "semantic search failed",
          });
        }
      }
    },
    [client],
  );

  useEffect(() => {
    if (!resolvedRepository || request.repoId === resolvedRepository.id) return;
    const canonical = new URLSearchParams(searchParams);
    canonical.set("repo", resolvedRepository.id);
    setSearchParams(canonical, { replace: true });
  }, [request.repoId, resolvedRepository, searchParams, setSearchParams]);

  useEffect(() => {
    setForm({ repoId: resolvedRepository?.id ?? "", query: request.query });
    if (client && isBounded && request.repoId === resolvedRepository?.id) {
      void load({ ...request, repoId: resolvedRepository.id });
    } else {
      // Leaving bounded state (e.g. back-navigating to an unbounded URL) must
      // also invalidate any in-flight search, or its late response would still
      // match latestLoad and overwrite this idle state with stale results.
      latestLoad.current += 1;
      setResult({ status: "idle" });
    }
  }, [client, isBounded, load, request, resolvedRepository]);

  // announceRef moves focus to the outcome announcement once a search settles
  // (ready or error), so a keyboard/screen-reader user who just submitted the
  // form is taken straight to the result instead of having to hunt for it.
  const announceRef = useRef<HTMLParagraphElement>(null);
  useEffect(() => {
    if (result.status === "ready" || result.status === "error") {
      announceRef.current?.focus();
    }
  }, [result.status]);

  // submit runs a fresh search: repo/query come from the form, and the
  // language selection resets since a new query invalidates the prior facet
  // set (a different query over the same repo can carry a different language
  // mix entirely).
  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const next = new URLSearchParams();
    if (form.repoId.trim()) next.set("repo", form.repoId.trim());
    if (form.query.trim()) next.set("q", form.query.trim());
    setSearchParams(next);
  }

  function updateDraft(update: (current: FormState) => FormState): void {
    latestLoad.current += 1;
    setResult({ status: "idle" });
    setForm(update);
  }

  // toggleLanguage flips one language in the URL-held selection and lets the
  // request-effect above re-query automatically (mirrors RelationshipsPage's
  // applyToolFilter, generalized to a multi-select set).
  function toggleLanguage(language: string): void {
    const next = new URLSearchParams(searchParams);
    const selected = new Set(request.languages);
    if (selected.has(language)) selected.delete(language);
    else selected.add(language);
    if (selected.size > 0) {
      next.set("languages", Array.from(selected).sort().join(","));
    } else {
      next.delete("languages");
    }
    setSearchParams(next, { replace: true });
  }

  // chipLanguages unions the current response's facet keys with whatever is
  // already selected, so a chip the user picked stays visible (and clearable)
  // even once it narrows the result set enough that a sibling language drops
  // out of facets.languages. See ToolFilter's identical "extra chip" handling
  // in RelationshipsPage.tsx.
  const facetCounts = useMemo<Readonly<Record<string, number>>>(
    () => (result.status === "ready" ? result.data.facets.languages : {}),
    [result],
  );
  const chipLanguages = useMemo(() => {
    const set = new Set<string>([...Object.keys(facetCounts), ...request.languages]);
    return Array.from(set).sort();
  }, [facetCounts, request.languages]);

  return (
    <div className="page semantic-search-page">
      <div className="page-intro">
        <h2>Semantic Search</h2>
        <p>
          Query the curated search-document index across code, repository files, runtime summaries,
          and semantic context for one repository. Narrow by language to focus the result set — the
          filter round-trips through the URL so a shared link restores the same view.
        </p>
      </div>

      <form className="semantic-search-form" onSubmit={submit} aria-label="Semantic search">
        <div className="semantic-search-field">
          <span>Repository</span>
          <SemanticRepositorySelector
            catalog={repositoryCatalog}
            onChange={(repoId) => updateDraft((current) => ({ ...current, repoId }))}
            searchHint={resolvedRepository ? "" : request.repoId}
            selectedRepositoryId={form.repoId}
          />
        </div>
        <label className="semantic-search-field">
          <span>Query</span>
          <input
            aria-label="Search query"
            className="popover-input"
            value={form.query}
            onChange={(event) =>
              updateDraft((current) => ({ ...current, query: event.target.value }))
            }
          />
        </label>
        <button
          className="btn-ghost active"
          type="submit"
          disabled={
            !hasLiveClient ||
            result.status === "loading" ||
            form.repoId.trim().length === 0 ||
            form.query.trim().length === 0
          }
        >
          {result.status === "loading" ? "Searching…" : "Search"}
        </button>
      </form>

      {!hasLiveClient ? (
        <p className="inline-state">Live Eshu API connection unavailable.</p>
      ) : null}
      {repositoryResolution.message && form.repoId === "" ? (
        <p className="inline-state semantic-repository-state" role="alert">
          {repositoryResolution.message}
        </p>
      ) : null}
      {hasLiveClient && !isBounded && !repositoryResolution.message ? (
        <p className="inline-state">Enter a repository and a query to search.</p>
      ) : null}
      {result.status === "error" ? (
        <p ref={announceRef} className="src-err" role="alert" tabIndex={-1}>
          ⚠ {result.message}
        </p>
      ) : null}

      {chipLanguages.length > 0 ? (
        <LanguageFacet
          languages={chipLanguages}
          counts={facetCounts}
          selected={request.languages}
          onToggle={toggleLanguage}
        />
      ) : null}

      {result.status === "ready" ? (
        <p ref={announceRef} className="sem-result-announce" role="status" tabIndex={-1}>
          {result.data.results.length} result{result.data.results.length === 1 ? "" : "s"} for
          &quot;{result.data.query}&quot;.
        </p>
      ) : null}

      <Panel
        title="Results"
        sub={
          result.status === "ready"
            ? `${result.data.results.length} of ${result.data.indexedDocumentCount} indexed documents`
            : undefined
        }
      >
        {result.status === "loading" ? <p className="empty">Searching…</p> : null}
        {result.status === "ready" && result.data.results.length === 0 ? (
          <p className="empty">No results for this query.</p>
        ) : null}
        {result.status === "ready" && result.data.results.length > 0 ? (
          <ol className="sem-result-list" aria-label="Search results">
            {result.data.results.map((item) => (
              <ResultRow key={item.document.id || `${item.rank}`} result={item} />
            ))}
          </ol>
        ) : null}
        {result.status === "idle" ? (
          <p className="empty">Run a search to see results here.</p>
        ) : null}
      </Panel>
    </div>
  );
}

function ResultRow({ result }: { readonly result: SemanticSearchResult }): React.JSX.Element {
  const doc = result.document;
  return (
    <li className="sem-result-row">
      <div className="sem-result-head">
        <span className="sem-result-rank">#{result.rank}</span>
        <h3 className="sem-result-title">{doc.title || doc.id}</h3>
        <span className="sem-result-tags">
          <TruthChip level={uiTruth(result.truthScope.level)} />
          <FreshDot state={uiFresh(result.freshness.state)} />
        </span>
      </div>
      {doc.path ? <p className="sem-result-path mono">{doc.path}</p> : null}
      {doc.contextText ? <p className="sem-result-snippet">{doc.contextText}</p> : null}
      <p className="sem-result-meta t-mut">
        {doc.sourceKind || "document"} · {result.searchMethod || "match"} · score{" "}
        {result.score.toFixed(2)}
      </p>
    </li>
  );
}

function LanguageFacet({
  languages,
  counts,
  selected,
  onToggle,
}: {
  readonly languages: readonly string[];
  readonly counts: Readonly<Record<string, number>>;
  readonly selected: readonly string[];
  readonly onToggle: (language: string) => void;
}): React.JSX.Element {
  const selectedSet = new Set(selected);
  return (
    <div className="sem-lang-facet" role="group" aria-label="Filter by language">
      <span className="sem-lang-facet-label t-mut">Language:</span>
      {languages.map((language) => {
        const active = selectedSet.has(language);
        return (
          <button
            key={language}
            type="button"
            className={`sem-lang-chip${active ? " active" : ""}`}
            aria-pressed={active}
            onClick={() => onToggle(language)}
          >
            <span className="mono">{language}</span>
            <strong>{counts[language] ?? 0}</strong>
          </button>
        );
      })}
    </div>
  );
}

function requestFromSearch(params: URLSearchParams): {
  repoId: string;
  query: string;
  languages: readonly string[];
} {
  const languagesParam = params.get("languages") ?? "";
  return {
    repoId: params.get("repo") ?? "",
    query: params.get("q") ?? "",
    languages: languagesParam
      .split(",")
      .map((value) => value.trim())
      .filter((value) => value.length > 0),
  };
}

type RepositoryResolution =
  | { readonly status: "empty"; readonly message: string }
  | { readonly status: "resolved"; readonly message: ""; readonly repository: RepoListItem }
  | { readonly status: "unresolved"; readonly message: string };

function resolveRepository(
  catalog: RepositoryCatalogState,
  requestedValue: string,
): RepositoryResolution {
  const requested = requestedValue.trim();
  if (catalog.kind === "loading") {
    return { status: "unresolved", message: "Repository catalog is still loading." };
  }
  if (catalog.kind === "unavailable") {
    return {
      status: "unresolved",
      message: `Repository catalog unavailable: ${catalog.error}`,
    };
  }
  if (catalog.repositories.length === 0) {
    return {
      status: "unresolved",
      message: "No authorized repositories are available in this session.",
    };
  }
  if (requested === "") return { status: "empty", message: "" };

  const canonical = catalog.repositories.find((repository) => repository.id === requested);
  if (canonical) return { status: "resolved", message: "", repository: canonical };

  if (catalog.completeness === "truncated") {
    return {
      status: "unresolved",
      message: `Repository ${requested} cannot be resolved from this incomplete authorized session catalog. Authorization cannot be determined; choose an explicit canonical repository ID.`,
    };
  }

  const aliases = catalog.repositories.filter(
    (repository) => repository.name === requested || repository.repoSlug === requested,
  );
  if (aliases.length === 1 && aliases[0]) {
    return { status: "resolved", message: "", repository: aliases[0] };
  }
  if (aliases.length > 1) {
    return {
      status: "unresolved",
      message: `Repository label ${requested} matches multiple authorized repositories. Choose one explicitly.`,
    };
  }
  return {
    status: "unresolved",
    message: `Repository ${requested} is not present in this authorized session catalog.`,
  };
}
