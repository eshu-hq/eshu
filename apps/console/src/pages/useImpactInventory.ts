import { useCallback, useEffect, useRef, useState } from "react";

import type { EshuApiClient } from "../api/client";
import {
  loadImpactInventory,
  type InventoryDimension,
  type InventoryPage,
  type InventoryProfile,
} from "../api/eshuConsoleImpactInventory";

interface InventoryState {
  readonly groupBy: InventoryDimension;
  readonly profile: InventoryProfile;
  // offsets holds the offset of every page visited so far; the last entry is
  // the page on screen. Previous pops it, Next pushes the server next_offset.
  readonly offsets: readonly number[];
  readonly page: InventoryPage | null;
  readonly loading: boolean;
  readonly error: string | null;
}

const initialState: InventoryState = {
  groupBy: "service_id",
  profile: "precise",
  offsets: [0],
  page: null,
  loading: false,
  error: null,
};

// useImpactInventory reads one grouped-inventory page for the selected group
// and profile. It reads only while mounted, so the console does not pay for the
// grouped read until the By service view is opened. A late response from a
// superseded request is discarded.
export function useImpactInventory(client?: EshuApiClient) {
  const [state, setState] = useState<InventoryState>(initialState);
  const sequence = useRef(0);
  const { groupBy, profile, offsets } = state;
  const offset = offsets[offsets.length - 1] ?? 0;

  useEffect(() => {
    if (!client) return undefined;
    sequence.current += 1;
    const request = sequence.current;
    const abort = new AbortController();
    setState((current) => ({ ...current, loading: true, error: null }));
    loadImpactInventory(client, { groupBy, profile, offset }, { signal: abort.signal })
      .then((page) => {
        if (request !== sequence.current) return;
        setState((current) => ({ ...current, page, loading: false }));
      })
      .catch((error: unknown) => {
        if (request !== sequence.current) return;
        setState((current) => ({
          ...current,
          page: null,
          loading: false,
          error: error instanceof Error ? error.message : "Unable to load grouped inventory.",
        }));
      });
    return () => {
      sequence.current += 1;
      abort.abort();
    };
  }, [client, groupBy, profile, offset]);

  const selectGroupBy = useCallback((next: InventoryDimension) => {
    setState((current) => ({ ...current, groupBy: next, offsets: [0], page: null }));
  }, []);

  const selectProfile = useCallback((next: InventoryProfile) => {
    setState((current) => ({ ...current, profile: next, offsets: [0], page: null }));
  }, []);

  const nextPage = useCallback(() => {
    setState((current) => {
      const next = current.page?.nextOffset;
      if (current.loading || next === null || next === undefined) return current;
      return { ...current, offsets: [...current.offsets, next] };
    });
  }, []);

  const previousPage = useCallback(() => {
    setState((current) =>
      current.loading || current.offsets.length < 2
        ? current
        : { ...current, offsets: current.offsets.slice(0, -1) },
    );
  }, []);

  return { ...state, selectGroupBy, selectProfile, nextPage, previousPage };
}
