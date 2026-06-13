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
