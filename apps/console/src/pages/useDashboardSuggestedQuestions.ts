import { useEffect, useMemo, useRef, useState } from "react";

import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";
import {
  loadSourceBackedSuggestedQuestionsResult,
  type SuggestedQuestion,
} from "../api/suggestedQuestions";
import type { RepositoryCatalogState } from "../repositoryCatalogLifecycle";

interface SuggestedQuestionsOwner {
  readonly client: EshuApiClient;
  readonly key: string;
  readonly promise: ReturnType<typeof loadSourceBackedSuggestedQuestionsResult>;
}

export function useDashboardSuggestedQuestions(
  client: EshuApiClient | undefined,
  live: boolean,
  repositories: readonly RepoListItem[] | undefined,
  repositoryCatalog: RepositoryCatalogState | undefined,
): {
  readonly error: boolean;
  readonly failures: readonly string[];
  readonly questions: readonly SuggestedQuestion[];
} {
  const [questions, setQuestions] = useState<readonly SuggestedQuestion[]>([]);
  const [error, setError] = useState(false);
  const [failures, setFailures] = useState<readonly string[]>([]);
  const ownerRef = useRef<SuggestedQuestionsOwner | null>(null);
  const key = useMemo(
    () =>
      JSON.stringify({
        catalog: repositoryCatalog?.kind ?? "self-loaded",
        repositories: repositories?.map((repository) => repository.id) ?? [],
      }),
    [repositories, repositoryCatalog?.kind],
  );

  useEffect(() => {
    let cancelled = false;
    if (!client || !live || repositoryCatalog?.kind === "loading") {
      setQuestions([]);
      setError(false);
      setFailures([]);
      return () => {
        cancelled = true;
      };
    }
    setError(false);
    setQuestions([]);
    setFailures([]);
    let owner = ownerRef.current;
    if (!owner || owner.client !== client || owner.key !== key) {
      owner = {
        client,
        key,
        promise: loadSourceBackedSuggestedQuestionsResult(
          client,
          repositoryCatalog
            ? {
                repositories: repositoryCatalog.repositories,
                repositoryCatalogUnavailable: repositoryCatalog.kind === "unavailable",
              }
            : {},
        ),
      };
      ownerRef.current = owner;
    }
    void owner.promise
      .then((result) => {
        if (!cancelled) {
          setQuestions(result.questions);
          setFailures(result.failures);
          setError(result.failures.length > 0 && result.questions.length === 0);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setQuestions([]);
          setError(true);
          setFailures([]);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [client, key, live, repositoryCatalog]);

  return { error, failures, questions };
}
