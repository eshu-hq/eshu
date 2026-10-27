import { useCallback, useEffect, useRef, useState } from "react";

import type { EshuApiClient } from "../api/client";
import {
  loadVulnerabilityStatusPage,
  type VulnerabilityPage,
  type VulnerabilityStatus,
} from "../api/eshuConsoleVulnerabilities";
import type { ConsoleModel } from "../console/types";

interface PageState {
  readonly status: VulnerabilityStatus;
  readonly index: number;
  readonly cursors: readonly (string | null)[];
  readonly page: VulnerabilityPage | null;
  readonly loading: boolean;
  readonly error: string | null;
}

function initialState(model: ConsoleModel): PageState {
  return {
    status: "affected_exact",
    index: 0,
    cursors: [null],
    page: model.vulnerabilityPages?.affected_exact ?? null,
    loading: false,
    error: null,
  };
}

export function useReachableFindingPage(model: ConsoleModel, client?: EshuApiClient) {
  const [state, setState] = useState<PageState>(() => initialState(model));
  const requestSequence = useRef(0);
  const controller = useRef<AbortController | null>(null);

  const cancel = useCallback(() => {
    requestSequence.current += 1;
    controller.current?.abort();
    controller.current = null;
  }, []);

  useEffect(() => {
    cancel();
    setState(initialState(model));
    return cancel;
  }, [model, client, cancel]);

  const selectStatus = useCallback(
    (status: VulnerabilityStatus) => {
      cancel();
      setState({
        status,
        index: 0,
        cursors: [null],
        page: model.vulnerabilityPages?.[status] ?? null,
        loading: false,
        error: null,
      });
    },
    [cancel, model],
  );

  const loadPage = useCallback(
    async (direction: "next" | "previous") => {
      if (!client || !state.page || state.loading) return;
      const targetIndex = state.index + (direction === "next" ? 1 : -1);
      if (targetIndex < 0 || (direction === "next" && !state.page.truncated)) return;
      const cursor = direction === "next" ? state.page.nextCursor : state.cursors[targetIndex];
      if (direction === "next" && (!cursor || state.cursors.includes(cursor))) {
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
        const page = await loadVulnerabilityStatusPage(client, names, state.status, cursor, {
          signal: abort.signal,
        });
        if (request !== requestSequence.current) return;
        setState((current) => ({
          ...current,
          index: targetIndex,
          cursors: direction === "next" ? [...current.cursors, cursor] : current.cursors,
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
    [cancel, client, model.repositoryNames, state],
  );

  return { ...state, selectStatus, loadPage };
}
