import { useEffect, useRef, useState, type FormEvent } from "react";
import { useLocation } from "react-router";

import type { EshuApiClient } from "../api/client";
import {
  searchFileContent,
  type FileSearchRequest,
  type FileSearchResponse,
  type FileSearchResult,
} from "../api/contentSearch";
import type { RepositoryCatalogState } from "../repositoryCatalogLifecycle";
import { loadingRepositoryCatalog } from "../repositoryCatalogLifecycle";

export interface ContentSearchNavigationState {
  readonly query?: string;
  readonly repoId?: string;
  readonly repoName?: string;
  readonly limit?: number;
}

type SearchState =
  | { readonly status: "idle" }
  | { readonly status: "loading"; readonly previous?: ReadyState }
  | { readonly status: "error"; readonly message: string }
  | ReadyState;

interface ReadyState {
  readonly status: "ready";
  readonly page: FileSearchResponse;
  readonly request: FileSearchRequest;
  readonly results: readonly FileSearchResult[];
  readonly initialOffset: number;
  readonly targetLimit: number;
  readonly hasMore: boolean;
  readonly pass: number;
}

export function ContentSearchPage({
  client,
  repositoryCatalog = loadingRepositoryCatalog,
}: {
  readonly client?: EshuApiClient;
  readonly repositoryCatalog?: RepositoryCatalogState;
}): React.JSX.Element {
  const location = useLocation();
  const navigation = location.state as ContentSearchNavigationState | null;
  const [query, setQuery] = useState(navigation?.query ?? "");
  const [repoId, setRepoId] = useState(navigation?.repoId ?? "");
  const [limit, setLimit] = useState(navigation?.limit ?? 50);
  const [unresolvedScope, setUnresolvedScope] = useState("");
  const [state, setState] = useState<SearchState>({ status: "idle" });
  const generation = useRef(0);

  useEffect(() => {
    generation.current += 1;
    setState({ status: "idle" });
  }, [client]);

  useEffect(() => {
    generation.current += 1;
    setState({ status: "idle" });
    setQuery(navigation?.query ?? "");
    setRepoId(navigation?.repoId ?? "");
    setLimit(navigation?.limit ?? 50);
    setUnresolvedScope(navigation?.repoName && !navigation.repoId ? navigation.repoName : "");
  }, [location.key, navigation]);

  useEffect(() => {
    if (
      !unresolvedScope ||
      repositoryCatalog.kind !== "ready" ||
      navigation?.repoName !== unresolvedScope ||
      navigation.repoId
    )
      return;
    const matches = repositoryCatalog.repositories.filter(
      (repository) => repository.name === unresolvedScope,
    );
    if (matches.length !== 1) return;
    setRepoId(matches[0].id);
    setUnresolvedScope("");
  }, [navigation, repositoryCatalog, unresolvedScope]);

  function clearResults(): void {
    generation.current += 1;
    setState({ status: "idle" });
  }

  async function run(request: FileSearchRequest, previous?: ReadyState): Promise<void> {
    if (!client) return;
    const current = ++generation.current;
    setState({ status: "loading", previous });
    try {
      const page = await searchFileContent(client, request);
      if (generation.current !== current) return;
      const prior = previous?.results ?? [];
      const targetLimit = previous?.targetLimit ?? page.limit;
      const remaining = Math.max(0, targetLimit - prior.length);
      const appended = page.results.slice(0, remaining);
      const hasMore = page.truncated || (remaining === 0 && page.results.length > 0);
      setState({
        status: "ready",
        page,
        request,
        results: [...prior, ...appended],
        initialOffset: previous?.initialOffset ?? request.offset ?? 0,
        targetLimit,
        hasMore,
        pass: (previous?.pass ?? 0) + 1,
      });
    } catch (error) {
      if (generation.current === current) {
        setState({
          status: "error",
          message: error instanceof Error ? error.message : "File search failed",
        });
      }
    }
  }

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    if (!query.trim() || unresolvedScope) return;
    void run({ query: query.trim(), repoId: repoId || undefined, limit, offset: 0 });
  }

  function resume(current: ReadyState): void {
    if (!current.page.resume) return;
    const remaining = Math.max(1, current.targetLimit - current.results.length);
    void run(
      {
        query: current.request.query,
        cursor: current.page.resume.cursor,
        limit: remaining,
        offset: current.page.resume.offset,
      },
      current,
    );
  }

  function nextPage(current: ReadyState): void {
    void run({
      query: current.request.query,
      repoId: current.request.repoId,
      limit: current.targetLimit,
      offset: current.initialOffset + current.results.length,
    });
  }

  const ready =
    state.status === "ready" ? state : state.status === "loading" ? state.previous : undefined;
  return (
    <div className="page content-search-page">
      <div className="page-intro">
        <h2>File content search</h2>
        <p>
          Search indexed file contents. An all-repository search may stop at its work budget; the
          result explains how far this request got.
        </p>
      </div>
      <form aria-label="File content search" className="semantic-search-form" onSubmit={submit}>
        <label className="semantic-search-field">
          <span>Content pattern</span>
          <input
            aria-label="Content pattern"
            className="popover-input"
            value={query}
            onChange={(event) => {
              clearResults();
              setQuery(event.target.value);
            }}
          />
        </label>
        <label className="semantic-search-field">
          <span>Repository</span>
          <select
            aria-label="Repository"
            className="popover-input"
            value={repoId}
            onChange={(event) => {
              clearResults();
              setUnresolvedScope("");
              setRepoId(event.target.value);
            }}
          >
            <option value="">All authorized repositories</option>
            {repositoryCatalog.repositories.map((repo) => (
              <option key={repo.id} value={repo.id}>
                {repo.name}
              </option>
            ))}
          </select>
        </label>
        <label className="semantic-search-field">
          <span>Page size</span>
          <input
            aria-label="Page size"
            className="popover-input"
            type="number"
            min={1}
            max={200}
            value={limit}
            onChange={(event) => {
              clearResults();
              setLimit(Number(event.target.value));
            }}
          />
        </label>
        <button
          className="btn-ghost active"
          type="submit"
          disabled={
            !client ||
            !query.trim() ||
            unresolvedScope !== "" ||
            limit < 1 ||
            limit > 200 ||
            state.status === "loading"
          }
        >
          {state.status === "loading" ? "Searching…" : "Fresh search"}
        </button>
      </form>
      {!client && <p role="status">Connect to the API to search file content.</p>}
      {unresolvedScope && (
        <p role="alert">
          Repository {unresolvedScope} is unavailable in this session. Select an authorized
          repository before searching.
        </p>
      )}
      {state.status === "error" && <p role="alert">{state.message}</p>}
      {ready && (
        <section aria-label="File content results">
          {ready.page.incomplete ? (
            <div role="status">
              {ready.page.partial ? (
                <>
                  <p>
                    Partial result, pass {ready.pass}: scanned{" "}
                    {ready.page.partial.rowsScannedInOrder} indexed files
                    {ready.page.documentsNotSearched >= 0
                      ? " of " +
                        (ready.page.partial.rowsScannedInOrder + ready.page.documentsNotSearched)
                      : "; total remaining is unknown"}{" "}
                    in this pass.
                  </p>
                  <p>
                    {ready.page.partial.progressed
                      ? "Only the searched prefix is represented. Resume from the cursor to continue."
                      : "This pass made no progress. An unchanged resume would repeat the same request."}
                  </p>
                  <p>{ready.page.partial.hint}</p>
                </>
              ) : (
                <p>
                  Partial result: unread file count{" "}
                  {ready.page.documentsNotSearched < 0
                    ? "unknown"
                    : ready.page.documentsNotSearched}
                  . No resume cursor was returned; try a repository scope.
                </p>
              )}
              {ready.page.resume && (
                <button
                  type="button"
                  onClick={() => resume(ready)}
                  disabled={state.status === "loading"}
                >
                  Resume search
                </button>
              )}
            </div>
          ) : (
            <p role="status">
              Exact page: {ready.results.length} matches shown.
              {ready.hasMore ? " More matches follow this page." : " Search complete."}
            </p>
          )}
          {ready.results.length === 0 && <p>No matches in the searched portion.</p>}
          <ul className="content-search-results">
            {ready.results.map((result) => (
              <li key={result.repoId + ":" + result.relativePath}>
                <strong>{result.relativePath}</strong> <span>{result.repoId}</span>
                <small>
                  {result.language} · {result.lineCount} lines
                </small>
                <pre>{result.content.slice(0, 500)}</pre>
              </li>
            ))}
          </ul>
          {!ready.page.incomplete && ready.hasMore && (
            <button type="button" onClick={() => nextPage(ready)}>
              Next page
            </button>
          )}
        </section>
      )}
    </div>
  );
}
