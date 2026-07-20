import { useCallback, useEffect, useRef, useState } from "react";

import type { EshuApiClient } from "../api/client";
import { loadImpactReview } from "../api/impactReview";
import type { ImpactReview, ImpactReviewInput } from "../api/impactReviewTypes";
import type { GraphNode } from "../console/types";

export interface ImpactReviewLifecycle {
  readonly busy: boolean;
  readonly error: string;
  readonly load: (input: ImpactReviewInput) => void;
  readonly reset: () => void;
  readonly review: ImpactReview | null;
  readonly selectNode: (node: GraphNode | undefined) => void;
  readonly selectedNode: GraphNode | undefined;
}

/** Keeps only the newest URL-owned impact request eligible to update the page. */
export function useImpactReviewLifecycle(client: EshuApiClient | undefined): ImpactReviewLifecycle {
  const generation = useRef(0);
  const [review, setReview] = useState<ImpactReview | null>(null);
  const [selectedNode, setSelectedNode] = useState<GraphNode | undefined>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(
    () => () => {
      generation.current += 1;
    },
    [],
  );

  const reset = useCallback(() => {
    generation.current += 1;
    setReview(null);
    setSelectedNode(undefined);
    setError("");
    setBusy(false);
  }, []);

  const load = useCallback(
    (input: ImpactReviewInput) => {
      const requestGeneration = generation.current + 1;
      generation.current = requestGeneration;
      setReview(null);
      setSelectedNode(undefined);
      setError("");
      setBusy(true);
      if (client === undefined || input.target.trim().length === 0) {
        setBusy(false);
        return;
      }
      void loadImpactReview(client, input)
        .then(
          (loaded) => {
            if (generation.current !== requestGeneration) return;
            setReview(loaded);
            setSelectedNode(loaded.graph.nodes.find((node) => node.hero) ?? loaded.graph.nodes[0]);
          },
          (loadError: unknown) => {
            if (generation.current !== requestGeneration) return;
            setError(
              loadError instanceof Error ? loadError.message : "failed to load impact review",
            );
          },
        )
        .finally(() => {
          if (generation.current === requestGeneration) setBusy(false);
        });
    },
    [client],
  );

  return { busy, error, load, reset, review, selectNode: setSelectedNode, selectedNode };
}
