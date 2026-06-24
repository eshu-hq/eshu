import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import vm from "node:vm";

import { describe, expect, it } from "vitest";

interface DeadCodeLoaderClient {
  get(path: string): Promise<unknown>;
  post(path: string, body: unknown): Promise<unknown>;
}

interface DeadCodeLoaderWindow {
  ESHU_LIVE_PARITY: {
    loadDeadCode(client: DeadCodeLoaderClient): Promise<{
      readonly deadCode?: readonly {
        readonly repo: string;
        readonly repoId: string;
        readonly repoName: string;
      }[];
    } | null>;
  };
}

function repoRoot(): string {
  return process.cwd().endsWith("apps/console") ? resolve(process.cwd(), "../..") : process.cwd();
}

function loadDeadCodeWindow(): DeadCodeLoaderWindow {
  const win = { ESHU_LIVE_PARITY: {} } as DeadCodeLoaderWindow;
  const loaderPath = resolve(repoRoot(), "apps/console/prototype/eshu-console/console/live-dead-code-loader.js");
  vm.runInNewContext(readFileSync(loaderPath, "utf8"), { window: win, Number });
  return win;
}

describe("prototype dead-code loader", () => {
  it("hides unresolved repository ids from display labels while preserving source ids", async () => {
    const win = loadDeadCodeWindow();
    const client: DeadCodeLoaderClient = {
      async get(): Promise<unknown> {
        return { data: { repositories: [] } };
      },
      async post(): Promise<unknown> {
        return {
          data: {
            results: [{
              classification: "unused",
              entity_id: "content-entity:e1",
              file_path: "server/routes.ts",
              labels: ["Function"],
              name: "unusedRoute",
              repo_id: "repository:r_078043f1"
            }]
          },
          error: null,
          truth: { level: "derived" }
        };
      }
    };

    const model = await win.ESHU_LIVE_PARITY.loadDeadCode(client);

    expect(model?.deadCode?.[0]).toMatchObject({
      repo: "unresolved repository",
      repoId: "repository:r_078043f1",
      repoName: "unresolved repository"
    });
  });
});
