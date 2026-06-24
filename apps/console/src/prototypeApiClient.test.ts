import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import vm from "node:vm";

import { describe, expect, it } from "vitest";

interface PrototypeClient {
  get(path: string): Promise<unknown>;
  post(path: string, body?: unknown): Promise<unknown>;
}

interface PrototypeEshu {
  readonly EshuApiClient: new (options?: { readonly baseUrl?: string; readonly apiKey?: string }) => PrototypeClient;
}

interface PrototypeWindow {
  ESHU?: PrototypeEshu;
}

const PROTOTYPE_CLIENT_SCRIPTS = new Set([
  "console/data.js",
  "console/live-api-client.js",
  "console/live-client-envelope.js"
]);

function repoRoot(): string {
  return process.cwd().endsWith("apps/console") ? resolve(process.cwd(), "../..") : process.cwd();
}

function prototypeScriptSources(html: string): readonly string[] {
  const document = new DOMParser().parseFromString(html, "text/html");
  return Array.from(document.querySelectorAll("script[src]"))
    .map((script) => script.getAttribute("src"))
    .filter((src): src is string => src !== null && PROTOTYPE_CLIENT_SCRIPTS.has(src));
}

function prototypeScriptPaths(): readonly string[] {
  const htmlPath = resolve(repoRoot(), "apps/console/prototype/eshu-console/Eshu Console.html");
  const html = readFileSync(htmlPath, "utf8");
  const scripts = prototypeScriptSources(html);
  return scripts.map((src) => resolve(repoRoot(), "apps/console/prototype/eshu-console", src));
}

function loadPrototypeEshu(fetchImpl: (url: string, init?: unknown) => Promise<unknown>): PrototypeEshu {
  const win: PrototypeWindow = {};
  const context = {
    window: win,
    Math,
    Number,
    Boolean,
    Object,
    String,
    URL,
    location: { origin: "http://localhost:5174" },
    fetch: fetchImpl
  };
  for (const scriptPath of prototypeScriptPaths()) {
    vm.runInNewContext(readFileSync(scriptPath, "utf8"), context);
  }
  if (win.ESHU === undefined) throw new Error("prototype ESHU model did not load");
  return win.ESHU;
}

describe("prototype API client", () => {
  it("discovers prototype client scripts through an HTML parser", () => {
    const html = `
      <script src="console/data.js"></script >
      <script type="text/babel" src="console/app.jsx"></script>
      <script src="console/live-api-client.js"></script>
      <script src="console/live-client-envelope.js"></script>
    `;

    expect(prototypeScriptSources(html)).toEqual([
      "console/data.js",
      "console/live-api-client.js",
      "console/live-client-envelope.js"
    ]);
  });

  it("rejects Eshu error envelopes even when the envelope has only a message", async () => {
    const eshu = loadPrototypeEshu(async () => ({
      ok: true,
      async json() {
        return {
          data: { status: "ready" },
          error: { message: "index status query failed" },
          truth: { level: "exact" }
        };
      }
    }));

    const client = new eshu.EshuApiClient({ baseUrl: "/eshu-api/" });

    await expect(client.get("/api/v0/index-status")).rejects.toThrow("index status query failed");
  });

  it("preserves Eshu error envelope code and message for prototype live calls", async () => {
    const eshu = loadPrototypeEshu(async () => ({
      ok: true,
      async json() {
        return {
          data: { incoming: [], outgoing: [] },
          error: {
            code: "unsupported_capability",
            message: "code relationships unavailable"
          },
          truth: null
        };
      }
    }));

    const client = new eshu.EshuApiClient({ baseUrl: "/eshu-api/" });

    await expect(client.post("/api/v0/code/relationships", { entity_id: "content-entity:e1" }))
      .rejects.toThrow("unsupported_capability: code relationships unavailable");
  });
});
