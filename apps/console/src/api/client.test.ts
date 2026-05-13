import { EshuApiClient } from "./client";
import { vi } from "vitest";

describe("EshuApiClient", () => {
  it("requests canonical envelope responses from the configured base URL", async () => {
    const calls: Request[] = [];
    const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = new Request(input, init);
      calls.push(request);
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
  });

  it("supports same-origin proxy base URLs for the local console", async () => {
    const calls: Request[] = [];
    const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = new Request(input, init);
      calls.push(request);
      return Response.json({
        count: 1,
        repositories: [{ id: "repository:r_1", name: "mobius-tools" }]
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
    expect(payload.repositories[0]?.name).toBe("mobius-tools");
  });

  it("sends a bearer token when the local API requires auth", async () => {
    const calls: Request[] = [];
    const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = new Request(input, init);
      calls.push(request);
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
  });

  it("omits authorization when no token is configured", async () => {
    const calls: Request[] = [];
    const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = new Request(input, init);
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
        const request = new Request(input, init);
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
