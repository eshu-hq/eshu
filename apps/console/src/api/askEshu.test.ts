import type { EshuApiClient } from "./client";
import { askEshuQuestion } from "./askEshu";

describe("askEshuQuestion", () => {
  it("requires a repository scope before calling query endpoints", async () => {
    const calls: string[] = [];
    const client = clientFor((method, path) => {
      calls.push(`${method} ${path}`);
      throw new Error(`unexpected ${method} ${path}`);
    });

    const answer = await askEshuQuestion(client, {
      question: "How does checkout auth work?",
      repoId: ""
    });

    expect(answer.status).toBe("needs_scope");
    expect(answer.errors.map((error) => error.source)).toEqual(["scope"]);
    expect(calls).toEqual([]);
  });

  it("runs bounded semantic search and code topic investigation for a scoped question", async () => {
    const calls: Array<{ readonly body: unknown; readonly path: string }> = [];
    const client = clientFor((_method, path, body) => {
      calls.push({ body, path });
      if (path === "/api/v0/search/semantic") {
        return {
          data: semanticPayload(),
          error: null,
          truth: truthEnvelope("semantic_search.curated_retrieval", "hybrid")
        };
      }
      if (path === "/api/v0/code/topics/investigate") {
        return {
          data: topicPayload(),
          error: null,
          truth: truthEnvelope("code_search.topic_investigation", "content_index")
        };
      }
      throw new Error(`unexpected ${path}`);
    });

    const answer = await askEshuQuestion(client, {
      question: "How does checkout auth work?",
      repoId: "repository:r1"
    });

    expect(calls).toEqual([
      {
        path: "/api/v0/search/semantic",
        body: {
          limit: 5,
          mode: "hybrid",
          query: "How does checkout auth work?",
          repo_id: "repository:r1",
          timeout_ms: 5000
        }
      },
      {
        path: "/api/v0/code/topics/investigate",
        body: {
          limit: 5,
          query: "How does checkout auth work?",
          repo_id: "repository:r1"
        }
      }
    ]);
    expect(answer.status).toBe("answered");
    expect(answer.answerPacket?.summary).toBe("Found 2 ranked code-topic evidence group(s).");
    expect(answer.semantic.results[0]?.title).toBe("Checkout auth flow");
    expect(answer.codeTopic.evidenceGroups[0]?.sourceHandle?.relativePath).toBe("src/auth.ts");
    expect(answer.errors).toEqual([]);
  });

  it("hydrates answer citations and derives an evidence subgraph from returned handles", async () => {
    const calls: Array<{ readonly body: unknown; readonly path: string }> = [];
    const client = clientFor((_method, path, body) => {
      calls.push({ body, path });
      if (path === "/api/v0/search/semantic") {
        return {
          data: semanticPayload(),
          error: null,
          truth: truthEnvelope("semantic_search.curated_retrieval", "hybrid")
        };
      }
      if (path === "/api/v0/code/topics/investigate") {
        return {
          data: topicPayloadWithAnswerHandles(),
          error: null,
          truth: truthEnvelope("code_search.topic_investigation", "content_index")
        };
      }
      if (path === "/api/v0/evidence/citations") {
        return {
          data: citationPayload(),
          error: null,
          truth: truthEnvelope("evidence_citation.packet", "content_index")
        };
      }
      if (path === "/api/v0/visualizations/derive") {
        return {
          data: visualizationPayload(),
          error: null,
          truth: truthEnvelope("evidence_citation.packet", "content_index")
        };
      }
      throw new Error(`unexpected ${path}`);
    });

    const answer = await askEshuQuestion(client, {
      question: "How does checkout auth work?",
      repoId: "repository:r1"
    });

    expect(calls.map((call) => call.path)).toEqual([
      "/api/v0/search/semantic",
      "/api/v0/code/topics/investigate",
      "/api/v0/evidence/citations",
      "/api/v0/visualizations/derive"
    ]);
    expect(calls[2]?.body).toMatchObject({
      limit: 10,
      question: "How does checkout auth work?"
    });
    expect((calls[2]?.body as { readonly handles: readonly unknown[] }).handles).toEqual([
      expect.objectContaining({
        kind: "file",
        relative_path: "src/auth.ts",
        repo_id: "repository:r1",
        start_line: 42
      }),
      expect.objectContaining({
        entity_id: "entity:auth",
        kind: "entity",
        repo_id: "repository:r1"
      })
    ]);
    expect(calls[3]?.body).toMatchObject({
      source_response: {
        citations: [{ citation_id: "citation:auth" }]
      },
      view: "evidence_citation"
    });
    expect(answer.answerPacket.summary).toBe("Checkout auth is backed by authorizeCheckout.");
    expect(answer.citationPacket?.citations[0]?.relativePath).toBe("src/auth.ts");
    expect(answer.visualizationPacket?.supported).toBe(true);
    expect(answer.answerGraph.nodes.map((node) => node.label)).toEqual(["authorizeCheckout"]);
  });

  it("surfaces partial endpoint failures instead of hiding them", async () => {
    const client = clientFor((_method, path) => {
      if (path === "/api/v0/search/semantic") {
        return {
          data: null,
          error: {
            code: "backend_unavailable",
            message: "semantic search requires the persisted search index"
          },
          truth: null
        };
      }
      if (path === "/api/v0/code/topics/investigate") {
        return {
          data: topicPayload(),
          error: null,
          truth: truthEnvelope("code_search.topic_investigation", "content_index")
        };
      }
      throw new Error(`unexpected ${path}`);
    });

    const answer = await askEshuQuestion(client, {
      question: "How does checkout auth work?",
      repoId: "repository:r1"
    });

    expect(answer.status).toBe("partial");
    expect(answer.errors).toEqual([{
      message: "backend_unavailable: semantic search requires the persisted search index",
      source: "semantic"
    }]);
    expect(answer.answerPacket?.supported).toBe(true);
  });
});

function clientFor(
  respond: (method: "POST", path: string, body: unknown) => unknown
): EshuApiClient {
  return {
    post: async (path: string, body: unknown) => respond("POST", path, body)
  } as unknown as EshuApiClient;
}

function truthEnvelope(capability: string, basis: string): Record<string, unknown> {
  return {
    basis,
    capability,
    freshness: { state: "fresh" },
    level: "derived",
    profile: "production"
  };
}

function semanticPayload(): Record<string, unknown> {
  return {
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
  };
}

function topicPayload(): Record<string, unknown> {
  return {
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
    recommended_next_calls: [{
      args: { entity_id: "entity:auth", limit: 25, repo_id: "repository:r1" },
      tool: "get_code_relationship_story"
    }],
    searched_terms: ["checkout", "auth"],
    truncated: false
  };
}

function topicPayloadWithAnswerHandles(): Record<string, unknown> {
  return {
    ...topicPayload(),
    answer_metadata: {
      evidence_handles: [
        {
          entity_id: "entity:auth",
          evidence_family: "source",
          kind: "entity",
          repo_id: "repository:r1",
          reason: "route handler entity"
        }
      ]
    },
    answer_packet: {
      citation_ref: "eshu://evidence/citations/checkout-auth",
      evidence_handles: [
        {
          evidence_family: "source",
          kind: "file",
          reason: "route handler source",
          relative_path: "src/auth.ts",
          repo_id: "repository:r1",
          start_line: 42
        }
      ],
      limitations: ["bounded to top five topic matches"],
      partial: false,
      primary_route: "/api/v0/code/topics/investigate",
      primary_tool: "investigate_code_topic",
      prompt_family: "code.topic",
      question: "How does checkout auth work?",
      recommended_next_calls: [{
        reason: "hydrate source citations",
        tool: "build_evidence_citation_packet"
      }],
      summary: "Checkout auth is backed by authorizeCheckout.",
      supported: true,
      truth_class: "code_hint"
    }
  };
}

function citationPayload(): Record<string, unknown> {
  return {
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
  };
}

function visualizationPayload(): Record<string, unknown> {
  return {
    visualization_packet: {
      edges: [],
      limits: {
        edge_count: 0,
        max_edges: 120,
        max_nodes: 60,
        node_count: 1,
        ordering: "stable_id"
      },
      limitations: [],
      nodes: [{
        category: "source",
        evidence_handle: {
          kind: "file",
          relative_path: "src/auth.ts",
          repo_id: "repository:r1",
          start_line: 42
        },
        id: "viznode:auth",
        label: "authorizeCheckout",
        type: "citation"
      }],
      recommended_next_calls: [],
      supported: true,
      title: "Checkout auth evidence",
      truncation: {
        dropped_edge_count: 0,
        dropped_node_count: 0,
        truncated: false
      },
      view: "evidence_citation"
    }
  };
}
