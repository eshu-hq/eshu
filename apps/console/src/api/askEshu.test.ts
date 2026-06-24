import { describe, expect, it, vi } from "vitest";

import { askEshu, askNarrationStatus, type AskAnswer, type AskError, type AskTraceStep } from "./askEshu";
import type { EshuFetcher } from "./client";

function sseResponse(chunks: readonly string[], status = 200): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk));
      }
      controller.close();
    }
  });
  return new Response(body, { status, headers: { "Content-Type": "text/event-stream" } });
}

function jsonResponse(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" }
  });
}

const connection = { baseUrl: "https://eshu.example/api/", apiKey: "shared-token" } as const;

describe("askEshu (streaming)", () => {
  it("emits trace steps, then the normalized answer, then done", async () => {
    const traces: AskTraceStep[] = [];
    let answer: AskAnswer | null = null;
    const done = vi.fn();
    const fetcher = vi.fn<EshuFetcher>(async () =>
      sseResponse([
        'event: trace\ndata: {"tool":"resolve_entity","supported":true,"truth_class":"deterministic"}\n\n',
        'event: trace\ndata: {"tool":"graph_query","supported":false,"truth_class":"fallback","err":"timeout"}\n\n',
        'event: answer\ndata: {"answer_prose":"Hello","truth_class":"derived","partial":true,"limitations":["stale"]}\n\n',
        "event: done\ndata: {}\n\n"
      ])
    );

    await new Promise<void>((resolve) => {
      askEshu({
        connection: { ...connection, fetcher },
        question: "How does auth work?",
        format: "auto",
        stream: true,
        onTrace: (step) => traces.push(step),
        onAnswer: (value) => {
          answer = value;
        },
        onDone: () => {
          done();
          resolve();
        }
      });
    });

    expect(traces).toHaveLength(2);
    expect(traces[0].tool).toBe("resolve_entity");
    expect(traces[1].supported).toBe(false);
    expect(done).toHaveBeenCalledTimes(1);
    const settled = answer as AskAnswer | null;
    expect(settled).not.toBeNull();
    expect(settled?.answer_prose).toBe("Hello");
    expect(settled?.partial).toBe(true);
    expect(settled?.limitations).toEqual(["stale"]);
    // Missing arrays are normalized to empty, never undefined.
    expect(settled?.artifacts).toEqual([]);
    expect(settled?.evidence_handles).toEqual([]);
    expect(settled?.query_trace).toEqual([]);

    const request = fetcher.mock.calls[0];
    const init = request[1] as RequestInit;
    expect((init.headers as Record<string, string>).Accept).toBe("text/event-stream");
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer shared-token");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({
      question: "How does auth work?",
      format: "auto"
    });
  });

  it("joins multi-line SSE data fields before parsing", async () => {
    let answer: AskAnswer | null = null;
    const fetcher = vi.fn(async () =>
      sseResponse([
        'event: answer\ndata: {"answer_prose":\ndata: "Split across lines"}\n\n',
        "event: done\ndata: {}\n\n"
      ])
    );
    await new Promise<void>((resolve) => {
      askEshu({
        connection: { ...connection, fetcher },
        question: "q",
        format: "auto",
        onAnswer: (value) => {
          answer = value;
        },
        onDone: () => resolve()
      });
    });
    expect((answer as AskAnswer | null)?.answer_prose).toBe("Split across lines");
  });

  it("preserves the wire state from an SSE error event", async () => {
    let error: AskError | null = null;
    const fetcher = vi.fn(async () =>
      sseResponse([
        'event: error\ndata: {"state":"forbidden","reason":"scoped token"}\n\n',
        "event: done\ndata: {}\n\n"
      ])
    );
    await new Promise<void>((resolve) => {
      askEshu({
        connection: { ...connection, fetcher },
        question: "q",
        format: "auto",
        onError: (value) => {
          error = value;
        },
        onDone: () => resolve()
      });
    });
    expect((error as AskError | null)?.state).toBe("forbidden");
    expect((error as AskError | null)?.reason).toBe("scoped token");
  });

  it("maps a 403 to a forbidden error and still signals done", async () => {
    let error: AskError | null = null;
    const done = vi.fn();
    const fetcher = vi.fn(async () => new Response("forbidden", { status: 403 }));

    await new Promise<void>((resolve) => {
      askEshu({
        connection: { ...connection, fetcher },
        question: "q",
        format: "auto",
        onError: (value) => {
          error = value;
        },
        onDone: () => {
          done();
          resolve();
        }
      });
    });

    expect((error as AskError | null)?.state).toBe("forbidden");
    expect(done).toHaveBeenCalledTimes(1);
  });

  it("maps a 503 to an unavailable error carrying the server reason", async () => {
    let error: AskError | null = null;
    const fetcher = vi.fn(async () =>
      jsonResponse({ state: "unavailable", reason: "Ask is off" }, 503)
    );

    await new Promise<void>((resolve) => {
      askEshu({
        connection: { ...connection, fetcher },
        question: "q",
        format: "auto",
        onError: (value) => {
          error = value;
        },
        onDone: () => resolve()
      });
    });

    expect((error as AskError | null)?.state).toBe("unavailable");
    expect((error as AskError | null)?.reason).toContain("Ask is off");
  });

  it("fires onAbort without onDone when the request is cancelled", async () => {
    const onAbort = vi.fn();
    const onDone = vi.fn();
    const onError = vi.fn();
    const fetcher = vi.fn(async () => {
      throw new DOMException("aborted", "AbortError");
    });

    await new Promise<void>((resolve) => {
      askEshu({
        connection: { ...connection, fetcher },
        question: "q",
        format: "auto",
        onAbort: () => {
          onAbort();
          resolve();
        },
        onError,
        onDone
      });
    });

    expect(onAbort).toHaveBeenCalledTimes(1);
    expect(onDone).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
  });

  it("maps a 400 to a bad_request error", async () => {
    let error: AskError | null = null;
    const fetcher = vi.fn(async () => new Response("bad", { status: 400 }));
    await new Promise<void>((resolve) => {
      askEshu({
        connection: { ...connection, fetcher },
        question: "",
        format: "auto",
        onError: (value) => {
          error = value;
        },
        onDone: () => resolve()
      });
    });
    expect((error as AskError | null)?.state).toBe("bad_request");
  });

  it("falls back to the synchronous JSON path when the SSE variant returns 500", async () => {
    // Issue #3381: a middleware that strips http.Flusher makes the SSE variant
    // 500 with "streaming not supported by this server configuration". The
    // synchronous JSON variant of the same route does not need flushing, so the
    // client must retry it instead of surfacing the 500.
    const traces: AskTraceStep[] = [];
    let answer: AskAnswer | null = null;
    const onError = vi.fn();
    const done = vi.fn();
    const fetcher = vi
      .fn<EshuFetcher>()
      .mockResolvedValueOnce(
        jsonResponse(
          { error: "internal_error", detail: "streaming not supported by this server configuration" },
          500
        )
      )
      .mockResolvedValueOnce(
        jsonResponse({
          answer_prose: "synchronous answer",
          truth_class: "derived",
          query_trace: [{ tool: "resolve_entity", supported: true, truth_class: "deterministic" }],
          partial: false,
          limitations: []
        })
      );

    await new Promise<void>((resolve) => {
      askEshu({
        connection: { ...connection, fetcher },
        question: "How does auth work?",
        format: "auto",
        stream: true,
        onTrace: (step) => traces.push(step),
        onAnswer: (value) => {
          answer = value;
        },
        onError,
        onDone: () => {
          done();
          resolve();
        }
      });
    });

    expect(onError).not.toHaveBeenCalled();
    expect((answer as AskAnswer | null)?.answer_prose).toBe("synchronous answer");
    expect(traces).toHaveLength(1);
    expect(done).toHaveBeenCalledTimes(1);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect((fetcher.mock.calls[0][1] as RequestInit).headers as Record<string, string>).toMatchObject({
      Accept: "text/event-stream"
    });
    expect((fetcher.mock.calls[1][1] as RequestInit).headers as Record<string, string>).toMatchObject({
      Accept: "application/json"
    });
  });

  it("surfaces an error when both the SSE and the sync fallback fail", async () => {
    // A genuine server error recurs on the sync path; the client must not loop
    // and must surface a single bounded error with exactly one onDone.
    const onError = vi.fn();
    const done = vi.fn();
    const fetcher = vi.fn<EshuFetcher>(async () => new Response("boom", { status: 500 }));

    await new Promise<void>((resolve) => {
      askEshu({
        connection: { ...connection, fetcher },
        question: "q",
        format: "auto",
        stream: true,
        onError,
        onDone: () => {
          done();
          resolve();
        }
      });
    });

    expect(onError).toHaveBeenCalledTimes(1);
    expect((onError.mock.calls[0][0] as AskError).state).toBe("error");
    expect(done).toHaveBeenCalledTimes(1);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
});

describe("askEshu (sync fallback)", () => {
  it("replays query_trace as trace steps and emits the answer", async () => {
    const traces: AskTraceStep[] = [];
    let answer: AskAnswer | null = null;
    const fetcher = vi.fn<EshuFetcher>(async () =>
      jsonResponse({
        answer_prose: "",
        truth_class: "unsupported",
        artifacts: [{ format: "json", content: "{}", issues: [] }],
        query_trace: [{ tool: "resolve_entity", supported: true, truth_class: "deterministic" }],
        partial: false,
        limitations: [],
        evidence_handles: [{ kind: "service", label: "checkout-api" }]
      })
    );

    await new Promise<void>((resolve) => {
      askEshu({
        connection: { ...connection, fetcher },
        question: "q",
        format: "json",
        stream: false,
        onTrace: (step) => traces.push(step),
        onAnswer: (value) => {
          answer = value;
        },
        onDone: () => resolve()
      });
    });

    expect(traces).toHaveLength(1);
    expect((answer as AskAnswer | null)?.artifacts).toHaveLength(1);
    expect((answer as AskAnswer | null)?.evidence_handles[0].label).toBe("checkout-api");
    const init = fetcher.mock.calls[0][1] as RequestInit;
    expect((init.headers as Record<string, string>).Accept).toBe("application/json");
  });

  it("uses browser-session cookies and CSRF when no Ask API key is configured", async () => {
    const cookieSpy = vi.spyOn(document, "cookie", "get").mockReturnValue("__Host-eshu_csrf=csrf-secret");
    let answer: AskAnswer | null = null;
    const fetcher = vi.fn<EshuFetcher>(async () =>
      jsonResponse({
        answer_prose: "cookie-backed answer",
        truth_class: "deterministic",
        partial: false,
        limitations: []
      })
    );
    try {
      await new Promise<void>((resolve) => {
        askEshu({
          connection: { baseUrl: "https://eshu.example/api/", apiKey: "", fetcher },
          question: "How does auth work?",
          format: "auto",
          stream: false,
          onAnswer: (value) => {
            answer = value;
          },
          onDone: () => resolve()
        });
      });
    } finally {
      cookieSpy.mockRestore();
    }

    expect((answer as AskAnswer | null)?.answer_prose).toBe("cookie-backed answer");
    const init = fetcher.mock.calls[0]?.[1] as RequestInit;
    expect(init.credentials).toBe("same-origin");
    expect((init.headers as Record<string, string>).Authorization).toBeUndefined();
    expect((init.headers as Record<string, string>)["X-Eshu-CSRF"]).toBe("csrf-secret");
  });
});

describe("askNarrationStatus", () => {
  it("reads state and reason from the status envelope", async () => {
    const fetcher = vi.fn(async () =>
      jsonResponse({ data: { state: "disabled", reason: "no provider" }, error: null, truth: null })
    );
    const probe = await askNarrationStatus({ ...connection, fetcher });
    expect(probe.state).toBe("disabled");
    expect(probe.reason).toBe("no provider");
  });

  it("reports providerConfigured=false when the ask adapter is not built", async () => {
    const fetcher = vi.fn(async () =>
      jsonResponse({
        data: { state: "unavailable", reason: "no provider", provider_configured: false },
        error: null,
        truth: null
      })
    );
    const probe = await askNarrationStatus({ ...connection, fetcher });
    expect(probe.state).toBe("unavailable");
    expect(probe.providerConfigured).toBe(false);
  });

  it("defaults providerConfigured to true when the field is absent", async () => {
    const fetcher = vi.fn(async () =>
      jsonResponse({ data: { state: "available" }, error: null, truth: null })
    );
    const probe = await askNarrationStatus({ ...connection, fetcher });
    expect(probe.providerConfigured).toBe(true);
  });

  it("falls back to unavailable when the probe request throws", async () => {
    const fetcher = vi.fn(async () => {
      throw new Error("network down");
    });
    const probe = await askNarrationStatus({ ...connection, fetcher });
    expect(probe.state).toBe("unavailable");
    expect(probe.reason).toContain("network down");
  });
});
