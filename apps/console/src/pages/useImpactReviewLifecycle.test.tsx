import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";

import { useImpactReviewLifecycle } from "./useImpactReviewLifecycle";
import { loadImpactReview } from "../api/impactReview";
import type { EshuApiClient } from "../api/client";
import type { ImpactReview, ImpactReviewInput } from "../api/impactReviewTypes";

vi.mock("../api/impactReview", () => ({ loadImpactReview: vi.fn() }));

const mockedLoadImpactReview = vi.mocked(loadImpactReview);

describe("useImpactReviewLifecycle", () => {
  beforeEach(() => mockedLoadImpactReview.mockReset());

  it("clears prior evidence while a new target loads and prevents an older response from replacing it", async () => {
    const alpha = deferred<ImpactReview>();
    const beta = deferred<ImpactReview>();
    mockedLoadImpactReview.mockReturnValueOnce(alpha.promise).mockReturnValueOnce(beta.promise);

    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Load alpha" }));
    fireEvent.click(screen.getByRole("button", { name: "Load beta" }));

    expect(screen.getByText("Loading")).toBeInTheDocument();
    expect(screen.getByText("No review")).toBeInTheDocument();
    expect(screen.getByText("No selection")).toBeInTheDocument();

    beta.resolve(review("beta"));
    await screen.findByText("Review beta");
    alpha.resolve(review("alpha"));

    await waitFor(() => expect(screen.getByText("Review beta")).toBeInTheDocument());
    expect(screen.queryByText("Review alpha")).not.toBeInTheDocument();
    expect(screen.getByText("Idle")).toBeInTheDocument();
  });

  it("ignores a stale rejection while surfacing a latest-request rejection", async () => {
    const alpha = deferred<ImpactReview>();
    const beta = deferred<ImpactReview>();
    mockedLoadImpactReview.mockReturnValueOnce(alpha.promise).mockReturnValueOnce(beta.promise);

    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Load alpha" }));
    fireEvent.click(screen.getByRole("button", { name: "Load beta" }));
    alpha.reject(new Error("alpha failed"));
    beta.reject(new Error("beta failed"));

    expect(await screen.findByText("beta failed")).toBeInTheDocument();
    expect(screen.queryByText("alpha failed")).not.toBeInTheDocument();
    expect(screen.getByText("Idle")).toBeInTheDocument();
  });
});

function Harness(): React.JSX.Element {
  const lifecycle = useImpactReviewLifecycle({} as EshuApiClient);
  return (
    <div>
      <button onClick={() => lifecycle.load(input("alpha"))} type="button">
        Load alpha
      </button>
      <button onClick={() => lifecycle.load(input("beta"))} type="button">
        Load beta
      </button>
      <p>{lifecycle.busy ? "Loading" : "Idle"}</p>
      <p>{lifecycle.review ? `Review ${lifecycle.review.input.target}` : "No review"}</p>
      <p>{lifecycle.selectedNode ? `Selected ${lifecycle.selectedNode.label}` : "No selection"}</p>
      {lifecycle.error ? <p>{lifecycle.error}</p> : null}
    </div>
  );
}

function input(target: string): ImpactReviewInput {
  return { target, targetKind: "service" };
}

function review(target: string): ImpactReview {
  return {
    blast: { reason: "not applicable", source: "blast", status: "skipped" },
    changeSurface: { error: "not loaded", source: "change", status: "unavailable" },
    deploymentTrace: { error: "not loaded", source: "trace", status: "unavailable" },
    graph: { edges: [], nodes: [{ col: 0, id: target, kind: "service", label: target }] },
    graphPresentation: {
      completeness: "unverified",
      compositionDurationMs: 0,
      duplicateEdges: 0,
      duplicateNodes: 0,
      edgeLimit: 0,
      inputEdges: 0,
      inputNodes: 1,
      limitations: [],
      mode: "empty",
      nodeLimit: 0,
      omittedEdges: 0,
      omittedNodes: 0,
      renderedEdges: 0,
      renderedNodes: 1,
      sourceApis: [],
      title: "Impact",
      truncated: false,
    },
    input: { limit: 25, maxDepth: 4, target, targetKind: "service" },
  };
}

function deferred<T>(): {
  readonly promise: Promise<T>;
  readonly reject: (reason: unknown) => void;
  readonly resolve: (value: T) => void;
} {
  let reject!: (reason: unknown) => void;
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}
