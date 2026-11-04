import { useCallback, useEffect, useRef, useState } from "react";

import type { EshuApiClient } from "../api/client";
import {
  loadVulnerabilityStatusPage,
  type ImpactListFilter,
  type ImpactListStatus,
  type VulnerabilityPage,
} from "../api/eshuConsoleVulnerabilities";
import type { ConsoleModel } from "../console/types";

interface PageState {
  readonly status: ImpactListStatus;
  readonly index: number;
  readonly cursors: readonly (string | null)[];
  readonly page: VulnerabilityPage | null;
  readonly loading: boolean;
  readonly error: string | null;
}

// seededPage returns the preloaded snapshot page for a status. The snapshot was
// read without a filter and without possibly_affected, so it only answers an
// unfiltered exact or derived list.
function seededPage(
  model: ConsoleModel,
  status: ImpactListStatus,
  filter: ImpactListFilter | null,
): VulnerabilityPage | null {
  if (filter !== null || status === "possibly_affected") return null;
  return model.vulnerabilityPages?.[status] ?? null;
}

function initialState(
  model: ConsoleModel,
  filter: ImpactListFilter | null,
  client?: EshuApiClient,
): PageState {
  return {
    status: "affected_exact",
    index: 0,
    cursors: [null],
    page: seededPage(model, "affected_exact", filter),
    loading: filter !== null && client !== undefined,
    error: null,
  };
}

export function useReachableFindingPage(
  model: ConsoleModel,
  client?: EshuApiClient,
  filter: ImpactListFilter | null = null,
) {
  // filterKey stands in for the filter object so a new object with the same
  // value does not refetch.
  const filterKey =
    filter === null ? "" : `${filter.key}=${filter.value}&profile=${filter.profile ?? "precise"}`;
  const [state, setState] = useState<PageState>(() => initialState(model, filter, client));
  const requestSequence = useRef(0);
  const controller = useRef<AbortController | null>(null);

  const cancel = useCallback(() => {
    requestSequence.current += 1;
    controller.current?.abort();
    controller.current = null;
  }, []);

  // loadFirstPage reads page one for a status when the snapshot cannot answer.
  const loadFirstPage = useCallback(
    async (status: ImpactListStatus) => {
      if (!client) return;
      cancel();
      const request = requestSequence.current;
      const abort = new AbortController();
      controller.current = abort;
      setState({ status, index: 0, cursors: [null], page: null, loading: true, error: null });
      try {
        const names = new Map(Object.entries(model.repositoryNames ?? {}));
        const page = await loadVulnerabilityStatusPage(
          client,
          names,
          status,
          null,
          { signal: abort.signal },
          filter,
        );
        if (request !== requestSequence.current) return;
        setState((current) => ({ ...current, page, loading: false }));
      } catch (error) {
        if (request !== requestSequence.current) return;
        setState((current) => ({
          ...current,
          loading: false,
          error: error instanceof Error ? error.message : "Unable to load findings page.",
        }));
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [cancel, client, model.repositoryNames, filterKey],
  );

  useEffect(() => {
    cancel();
    if (filter !== null && client) {
      void loadFirstPage("affected_exact");
    } else {
      setState(initialState(model, filter, client));
    }
    return cancel;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [model, client, cancel, filterKey, loadFirstPage]);

  const selectStatus = useCallback(
    (status: ImpactListStatus) => {
      const seeded = seededPage(model, status, filter);
      if (seeded === null && client) {
        void loadFirstPage(status);
        return;
      }
      cancel();
      setState({
        status,
        index: 0,
        cursors: [null],
        page: seeded,
        loading: false,
        error: null,
      });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [cancel, client, loadFirstPage, model, filterKey],
  );

  const loadPage = useCallback(
    async (direction: "next" | "previous") => {
      if (!client || !state.page || state.loading) return;
      const targetIndex = state.index + (direction === "next" ? 1 : -1);
      if (targetIndex < 0 || (direction === "next" && !state.page.truncated)) return;
      const cursor = direction === "next" ? state.page.nextCursor : state.cursors[targetIndex];
      if (direction === "next" && !cursor) {
        setState((current) => ({ ...current, error: "The next findings cursor did not advance." }));
        return;
      }
      cancel();
      const request = requestSequence.current;
      const abort = new AbortController();
      controller.current = abort;
      setState((current) => ({ ...current, loading: true, error: null }));
      try {
        const names = new Map(Object.entries(model.repositoryNames ?? {}));
        const page = await loadVulnerabilityStatusPage(
          client,
          names,
          state.status,
          cursor,
          { signal: abort.signal },
          filter,
        );
        if (request !== requestSequence.current) return;
        setState((current) => ({
          ...current,
          index: targetIndex,
          cursors:
            direction === "next"
              ? [...current.cursors.slice(0, targetIndex), cursor]
              : current.cursors,
          page,
          loading: false,
        }));
      } catch (error) {
        if (request !== requestSequence.current) return;
        setState((current) => ({
          ...current,
          loading: false,
          error: error instanceof Error ? error.message : "Unable to load findings page.",
        }));
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [cancel, client, model.repositoryNames, state, filterKey],
  );

  return { ...state, filter, selectStatus, loadPage };
}
