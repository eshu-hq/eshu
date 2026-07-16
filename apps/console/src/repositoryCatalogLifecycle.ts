import { useCallback, useRef, useState } from "react";

import type { EshuApiClient } from "./api/client";
import { loadRepositoryCatalog, type RepoListItem } from "./api/repoCatalog";

export type RepositoryCatalogState =
  | {
      readonly kind: "loading";
      readonly repositories: readonly RepoListItem[];
    }
  | {
      readonly kind: "ready";
      readonly completeness: "complete" | "truncated";
      readonly repositories: readonly RepoListItem[];
      readonly warning: string;
    }
  | {
      readonly kind: "unavailable";
      readonly error: string;
      readonly repositories: readonly RepoListItem[];
    };

export const loadingRepositoryCatalog: RepositoryCatalogState = {
  kind: "loading",
  repositories: [],
};

export function readyRepositoryCatalog(
  repositories: readonly RepoListItem[],
): RepositoryCatalogState {
  return { completeness: "complete", kind: "ready", repositories, warning: "" };
}

export function loadRepositoryCatalogState(client: EshuApiClient): Promise<RepositoryCatalogState> {
  return loadRepositoryCatalog(client).then(
    (catalog) => ({
      completeness: catalog.completeness,
      kind: "ready",
      repositories: catalog.repositories,
      warning: catalog.warning,
    }),
    (error: unknown) => ({
      error: error instanceof Error ? error.message : "repository catalog unavailable",
      kind: "unavailable",
      repositories: [],
    }),
  );
}

export function useRepositoryCatalogLifecycle(initial: RepositoryCatalogState): {
  readonly activate: (client: EshuApiClient, pending: Promise<RepositoryCatalogState>) => void;
  readonly clear: () => void;
  readonly replace: (state: RepositoryCatalogState) => void;
  readonly state: RepositoryCatalogState;
} {
  const [state, setState] = useState(initial);
  const owner = useRef<EshuApiClient | null>(null);

  const activate = useCallback(
    (client: EshuApiClient, pending: Promise<RepositoryCatalogState>): void => {
      owner.current = client;
      setState(loadingRepositoryCatalog);
      void pending.then((next) => {
        if (owner.current === client) setState(next);
      });
    },
    [],
  );
  const replace = useCallback((next: RepositoryCatalogState): void => {
    owner.current = null;
    setState(next);
  }, []);
  const clear = useCallback((): void => {
    owner.current = null;
    setState(loadingRepositoryCatalog);
  }, []);

  return { activate, clear, replace, state };
}
