import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import { AskPage } from "./AskPage";

describe("AskPage", () => {
  it("resets a stale repository scope when the live repository list changes", async () => {
    const calls: Array<{ readonly body: unknown; readonly path: string }> = [];
    const client = askClient((path, body) => calls.push({ body, path }));
    const { rerender } = render(
      <MemoryRouter>
        <AskPage
          client={client}
          repositories={[{ id: "repository:r1", name: "checkout-api" }]}
        />
      </MemoryRouter>
    );

    expect(screen.getByLabelText("Repository")).toHaveValue("repository:r1");

    rerender(
      <MemoryRouter>
        <AskPage
          client={client}
          repositories={[{ id: "repository:r2", name: "billing-api" }]}
        />
      </MemoryRouter>
    );

    expect(screen.getByLabelText("Repository")).toHaveValue("repository:r2");
    fireEvent.change(screen.getByLabelText("Question"), {
      target: { value: "How does billing auth work?" }
    });
    fireEvent.click(screen.getByRole("button", { name: "Ask" }));

    await waitFor(() => expect(calls).toHaveLength(2));
    expect(calls.map((call) => (call.body as Record<string, unknown>).repo_id)).toEqual([
      "repository:r2",
      "repository:r2"
    ]);
  });

  it("renders a scoped answer from code-topic and semantic-search endpoints", async () => {
    const client = askClient();

    render(
      <MemoryRouter>
        <AskPage
          client={client}
          repositories={[{ id: "repository:r1", name: "checkout-api" }]}
        />
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText("Question"), {
      target: { value: "How does checkout auth work?" }
    });
    fireEvent.click(screen.getByRole("button", { name: "Ask" }));

    expect(await screen.findByText("Found 2 ranked code-topic evidence group(s).")).toBeInTheDocument();
    expect(screen.getByText("code.topic")).toBeInTheDocument();
    expect(screen.getByText("semantic_search.curated_retrieval")).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: "src/auth.ts:42" })[0]).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar1/source?path=src%2Fauth.ts&lineStart=42"
    );
    expect(screen.getByText("Checkout auth flow")).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.queryByText("Choose a repository before asking.")).not.toBeInTheDocument();
    });
  });

  it("renders answer citations and a derived evidence subgraph", async () => {
    render(
      <MemoryRouter>
        <AskPage
          client={askClientWithAnswerEvidence()}
          repositories={[{ id: "repository:r1", name: "checkout-api" }]}
        />
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText("Question"), {
      target: { value: "How does checkout auth work?" }
    });
    fireEvent.click(screen.getByRole("button", { name: "Ask" }));

    expect(
      await screen.findByText("Checkout auth is backed by authorizeCheckout.")
    ).toBeInTheDocument();
    expect(screen.getAllByTitle("Truth: derived").length).toBeGreaterThan(0);
    expect(screen.getAllByTitle("Freshness: fresh").length).toBeGreaterThan(0);
    expect(screen.getAllByRole("link", { name: "src/auth.ts:42" })[0]).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar1/source?path=src%2Fauth.ts&lineStart=42"
    );
    expect(screen.getByText("1 nodes · 0 edges")).toBeInTheDocument();
    expect(screen.getAllByText("authorizeCheckout").length).toBeGreaterThan(0);
  });
});

function askClient(
  onPost?: (path: string, body: unknown) => void
): EshuApiClient {
  return {
    post: async (path: string, body: unknown) => {
      onPost?.(path, body);
      if (path === "/api/v0/search/semantic") {
        return {
          data: {
            indexed_document_count: 12,
            limit: 5,
            query: "How does checkout auth work?",
            repo_id: "repository:r1",
            results: [{
              document: {
                context_text: "checkout authentication validates session claims before payment.",
                id: "doc:checkout-auth",
                path: "src/auth.ts",
                source_kind: "code_entity",
                title: "Checkout auth flow"
              },
              freshness: { state: "fresh" },
              rank: 1,
              score: 12.4,
              search_method: "bm25",
              truth_scope: { basis: "content_index", level: "derived" }
            }],
            search_mode: "hybrid",
            truncated: false
          },
          error: null,
          truth: {
            basis: "hybrid",
            capability: "semantic_search.curated_retrieval",
            freshness: { state: "fresh" },
            level: "derived",
            profile: "production"
          }
        };
      }
      if (path === "/api/v0/code/topics/investigate") {
        return {
          data: {
            answer_packet: {
              partial: false,
              primary_route: "/api/v0/code/topics/investigate",
              primary_tool: "investigate_code_topic",
              prompt_family: "code.topic",
              question: "How does checkout auth work?",
              summary: "Found 2 ranked code-topic evidence group(s).",
              supported: true,
              truth_class: "code_hint"
            },
            count: 2,
            evidence_groups: [{
              entity_id: "entity:auth",
              entity_name: "authorizeCheckout",
              entity_type: "function",
              language: "typescript",
              rank: 1,
              relative_path: "src/auth.ts",
              score: 8,
              source_handle: {
                end_line: 50,
                relative_path: "src/auth.ts",
                repo_id: "repository:r1",
                start_line: 42
              },
              source_kind: "content_entity"
            }],
            recommended_next_calls: [],
            searched_terms: ["checkout", "auth"],
            truncated: false
          },
          error: null,
          truth: {
            basis: "content_index",
            capability: "code_search.topic_investigation",
            freshness: { state: "fresh" },
            level: "derived",
            profile: "production"
          }
        };
      }
      throw new Error(`unexpected ${path}`);
    }
  } as unknown as EshuApiClient;
}

function askClientWithAnswerEvidence(): EshuApiClient {
  return {
    post: async (path: string) => {
      if (path === "/api/v0/search/semantic") {
        return {
          data: {
            indexed_document_count: 0,
            results: [],
            truncated: false
          },
          error: null,
          truth: {
            basis: "hybrid",
            capability: "semantic_search.curated_retrieval",
            freshness: { state: "fresh" },
            level: "derived",
            profile: "production"
          }
        };
      }
      if (path === "/api/v0/code/topics/investigate") {
        return {
          data: {
            answer_packet: {
              evidence_handles: [{
                evidence_family: "source",
                kind: "file",
                reason: "route handler source",
                relative_path: "src/auth.ts",
                repo_id: "repository:r1",
                start_line: 42
              }],
              partial: false,
              primary_route: "/api/v0/code/topics/investigate",
              primary_tool: "investigate_code_topic",
              prompt_family: "code.topic",
              question: "How does checkout auth work?",
              summary: "Checkout auth is backed by authorizeCheckout.",
              supported: true,
              truth_class: "code_hint"
            },
            evidence_groups: [],
            recommended_next_calls: [],
            searched_terms: ["checkout", "auth"],
            truncated: false
          },
          error: null,
          truth: {
            basis: "content_index",
            capability: "code_search.topic_investigation",
            freshness: { state: "fresh" },
            level: "derived",
            profile: "production"
          }
        };
      }
      if (path === "/api/v0/evidence/citations") {
        return {
          data: {
            citations: [{
              citation_id: "citation:auth",
              end_line: 50,
              entity_id: "entity:auth",
              entity_name: "authorizeCheckout",
              entity_type: "function",
              evidence_family: "source",
              excerpt: "export function authorizeCheckout() { return session.valid; }",
              kind: "file",
              language: "typescript",
              rank: 1,
              reason: "route handler source",
              relative_path: "src/auth.ts",
              repo_id: "repository:r1",
              start_line: 42
            }],
            coverage: {
              input_handle_count: 1,
              limit: 10,
              missing_count: 0,
              query_shape: "bounded_evidence_citation_packet",
              resolved_count: 1,
              source_backend: "postgres_content_store",
              truncated: false
            },
            missing_handles: [],
            recommended_next_calls: []
          },
          error: null,
          truth: {
            basis: "content_index",
            capability: "evidence_citation.packet",
            freshness: { state: "fresh" },
            level: "derived",
            profile: "production"
          }
        };
      }
      if (path === "/api/v0/visualizations/derive") {
        return {
          data: {
            visualization_packet: {
              edges: [],
              limits: {
                edge_count: 0,
                max_edges: 120,
                max_nodes: 60,
                node_count: 1,
                ordering: "stable_id"
              },
              nodes: [{
                category: "source",
                id: "viznode:auth",
                label: "authorizeCheckout",
                type: "citation"
              }],
              supported: true,
              title: "Checkout auth evidence",
              truncation: {
                dropped_edge_count: 0,
                dropped_node_count: 0,
                truncated: false
              },
              view: "evidence_citation"
            }
          },
          error: null,
          truth: {
            basis: "content_index",
            capability: "evidence_citation.packet",
            freshness: { state: "fresh" },
            level: "derived",
            profile: "production"
          }
        };
      }
      throw new Error(`unexpected ${path}`);
    }
  } as unknown as EshuApiClient;
}
