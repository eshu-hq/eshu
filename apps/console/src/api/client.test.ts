import { EshuApiClient, EshuApiHttpError } from "./client";
import { inspectionRequest } from "../test/inspectionRequest";
import { expect, it, vi } from "vitest";

describe("EshuApiClient", () => {
  it("requests canonical envelope responses from the configured base URL", async () => {
    const calls: Request[] = [];
    const inits: (RequestInit | undefined)[] = [];
    const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = inspectionRequest(input, init);
      calls.push(request);
      inits.push(init);
      return Response.json({
        data: { status: "ok" },
        error: null,
        truth: {
          capability: "runtime.status",
          freshness: { state: "fresh" },
          level: "exact",
          profile: "local_full_stack"
        }
      });
    };

    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080/",
      fetcher
    });

    await client.get<{ readonly status: string }>("/api/v0/index-status");

    expect(calls).toHaveLength(1);
    expect(calls[0]?.url).toBe("http://localhost:8080/api/v0/index-status");
    expect(calls[0]?.headers.get("Accept")).toBe("application/eshu.envelope+json");
    expect(inits[0]?.credentials).toBe("same-origin");
  });

  it("supports same-origin proxy base URLs for the local console", async () => {
    const calls: Request[] = [];
    const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = inspectionRequest(input, init);
      calls.push(request);
      return Response.json({
        count: 1,
        repositories: [{ id: "repository:r_1", name: "platform-tools" }]
      });
    };

    const client = new EshuApiClient({
      baseUrl: "/eshu-api/",
      fetcher
    });

    const payload = await client.getJson<{
      readonly count: number;
      readonly repositories: readonly { readonly name: string }[];
    }>("/api/v0/repositories");

    expect(calls[0]?.url).toBe("http://localhost:5174/eshu-api/api/v0/repositories");
    expect(payload.repositories[0]?.name).toBe("platform-tools");
  });

  it("sends a bearer token when the local API requires auth", async () => {
    const calls: Request[] = [];
    const inits: (RequestInit | undefined)[] = [];
    const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = inspectionRequest(input, init);
      calls.push(request);
      inits.push(init);
      return Response.json({ status: "healthy" });
    };

    const client = new EshuApiClient({
      apiKey: "local-compose-token",
      baseUrl: "/eshu-api/",
      fetcher
    });

    await client.getJson("/api/v0/index-status");

    expect(calls[0]?.headers.get("Authorization")).toBe(
      "Bearer local-compose-token"
    );
    expect(inits[0]?.credentials).toBe("same-origin");
  });

  it("adds a CSRF header for unsafe cookie-session requests", async () => {
    const cookieSpy = vi.spyOn(document, "cookie", "get").mockReturnValue("__Host-eshu_csrf=csrf-secret");
    const calls: Request[] = [];
    const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = inspectionRequest(input, init);
      calls.push(request);
      return Response.json({
        data: { status: "ok" },
        error: null,
        truth: null
      });
    };

    try {
      const client = new EshuApiClient({
        baseUrl: "/eshu-api/",
        fetcher
      });

      await client.post("/api/v0/auth/browser-session/context", {
        tenant_id: "tenant_a",
        workspace_id: "workspace_a"
      });

      expect(calls[0]?.headers.get("X-Eshu-CSRF")).toBe("csrf-secret");
      expect(calls[0]?.headers.has("Authorization")).toBe(false);
    } finally {
      cookieSpy.mockRestore();
    }
  });

  it("does not add CSRF to explicit bearer-token requests", async () => {
    const cookieSpy = vi.spyOn(document, "cookie", "get").mockReturnValue("__Host-eshu_csrf=csrf-secret");
    const calls: Request[] = [];
    const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = inspectionRequest(input, init);
      calls.push(request);
      return Response.json({ status: "healthy" });
    };

    try {
      const client = new EshuApiClient({
        apiKey: "local-compose-token",
        baseUrl: "/eshu-api/",
        fetcher
      });

      await client.postJson("/api/v0/code/dead-code", { limit: 1 });

      expect(calls[0]?.headers.get("Authorization")).toBe(
        "Bearer local-compose-token"
      );
      expect(calls[0]?.headers.has("X-Eshu-CSRF")).toBe(false);
    } finally {
      cookieSpy.mockRestore();
    }
  });

  it("creates a browser session from an explicit bearer token", async () => {
    const calls: Request[] = [];
    const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = inspectionRequest(input, init);
      calls.push(request);
      return Response.json({
        auth: { mode: "browser_session", all_scopes: false },
        csrf_token: "csrf-secret"
      }, { status: 201 });
    };

    const client = new EshuApiClient({
      apiKey: "scoped-login-token",
      baseUrl: "/eshu-api/",
      fetcher
    });

    const session = await client.createBrowserSession();

    expect(session.csrf_token).toBe("csrf-secret");
    expect(calls[0]?.url).toBe("http://localhost:5174/eshu-api/api/v0/auth/browser-session");
    expect(calls[0]?.method).toBe("POST");
    expect(calls[0]?.headers.get("Authorization")).toBe("Bearer scoped-login-token");
    expect(calls[0]?.headers.has("X-Eshu-CSRF")).toBe(false);
  });

  it("revokes and switches browser sessions with CSRF proof", async () => {
    const cookieSpy = vi.spyOn(document, "cookie", "get").mockReturnValue("__Host-eshu_csrf=csrf-secret");
    const calls: Request[] = [];
    const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = inspectionRequest(input, init);
      calls.push(request);
      if (request.method === "DELETE") {
        return new Response(null, { status: 204 });
      }
      return Response.json({
        auth: {
          mode: "browser_session",
          tenant_id: "tenant_b",
          workspace_id: "workspace_b",
          all_scopes: true
        }
      });
    };

    try {
      const client = new EshuApiClient({
        baseUrl: "/eshu-api/",
        fetcher
      });

      await client.logoutBrowserSession();
      const switched = await client.switchBrowserSessionContext("tenant_b", "workspace_b");

      expect(calls[0]?.method).toBe("DELETE");
      expect(calls[0]?.headers.get("X-Eshu-CSRF")).toBe("csrf-secret");
      expect(calls[1]?.method).toBe("PATCH");
      expect(calls[1]?.headers.get("X-Eshu-CSRF")).toBe("csrf-secret");
      expect(switched.auth.workspace_id).toBe("workspace_b");
    } finally {
      cookieSpy.mockRestore();
    }
  });

  it("omits authorization when no token is configured", async () => {
    const calls: Request[] = [];
    const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = inspectionRequest(input, init);
      calls.push(request);
      return Response.json({ status: "healthy" });
    };

    const client = new EshuApiClient({
      apiKey: " ",
      baseUrl: "/eshu-api/",
      fetcher
    });

    await client.getJson("/api/v0/index-status");

    expect(calls[0]?.headers.has("Authorization")).toBe(false);
  });

  it("attaches an abort signal to every request so a hung endpoint cannot block forever", async () => {
    const inits: (RequestInit | undefined)[] = [];
    const fetcher = async (_input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      inits.push(init);
      return Response.json({ status: "healthy" });
    };

    const client = new EshuApiClient({ baseUrl: "/eshu-api/", fetcher });

    await client.getJson("/api/v0/index-status");
    await client.get("/api/v0/index-status");
    await client.post("/api/v0/code/dead-code", { limit: 1 });
    await client.postJson("/api/v0/code/dead-code", { limit: 1 });

    expect(inits).toHaveLength(4);
    for (const init of inits) {
      expect(init?.signal).toBeInstanceOf(AbortSignal);
    }
  });

  it("propagates the abort error when a request exceeds the configured timeout", async () => {
    const fetcher = (_input: RequestInfo | URL, init?: RequestInit): Promise<Response> =>
      new Promise((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => {
          reject(init.signal?.reason ?? new DOMException("aborted", "AbortError"));
        });
      });

    const client = new EshuApiClient({ baseUrl: "/eshu-api/", fetcher, timeoutMs: 5 });

    await expect(client.getJson("/api/v0/index-status")).rejects.toThrowError();
  });

  it("throws a typed EshuApiHttpError carrying the response status on non-2xx", async () => {
    const fetcher = async (): Promise<Response> =>
      new Response("not found", { status: 404 });
    const client = new EshuApiClient({ baseUrl: "/eshu-api/", fetcher });

    const error = await client
      .post("/api/v0/code/relationships", { entity_id: "workload:x" })
      .then(() => null)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(EshuApiHttpError);
    expect((error as EshuApiHttpError).status).toBe(404);
    expect((error as EshuApiHttpError).message).toContain("404");
  });

  it("preserves structured Eshu error envelopes on non-2xx responses", async () => {
    const fetcher = async (): Promise<Response> =>
      Response.json({
        data: null,
        error: {
          code: "unsupported_capability",
          message: "identity trust chains require local-authoritative profile"
        },
        truth: null
      }, { status: 501 });
    const client = new EshuApiClient({ baseUrl: "/eshu-api/", fetcher });

    const error = await client
      .get("/api/v0/secrets-iam/identity-trust-chains?scope_id=s&limit=25")
      .then(() => null)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(EshuApiHttpError);
    expect((error as EshuApiHttpError).status).toBe(501);
    expect((error as EshuApiHttpError).error?.code).toBe("unsupported_capability");
    expect((error as EshuApiHttpError).message).toContain("unsupported_capability");
  });

  it("throws a typed EshuApiHttpError from the JSON helpers too", async () => {
    const fetcher = async (): Promise<Response> =>
      new Response("boom", { status: 500 });
    const client = new EshuApiClient({ baseUrl: "/eshu-api/", fetcher });

    const error = await client
      .getJson("/api/v0/index-status")
      .then(() => null)
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(EshuApiHttpError);
    expect((error as EshuApiHttpError).status).toBe(500);
  });

  it("binds the browser fetch implementation when no custom fetcher is provided", async () => {
    const calls: Request[] = [];
    vi.stubGlobal(
      "fetch",
      function browserFetch(
        this: typeof globalThis,
        input: RequestInfo | URL,
        init?: RequestInit
      ): Promise<Response> {
        if (this !== globalThis) {
          throw new TypeError("Illegal invocation");
        }
        const request = inspectionRequest(input, init);
        calls.push(request);
        return Promise.resolve(Response.json({ status: "healthy" }));
      }
    );

    const client = new EshuApiClient({ baseUrl: "/eshu-api/" });

    await expect(client.getJson("/api/v0/index-status")).resolves.toEqual({
      status: "healthy"
    });
    expect(calls[0]?.url).toBe("http://localhost:5174/eshu-api/api/v0/index-status");
  });
});
