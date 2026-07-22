import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { decodeRepoFile, loadRepoBranches, loadRepoFile, loadRepoTree } from "./repoSource";

describe("repoSource", () => {
  it("loads repository branches as the derived indexed ref list", async () => {
    let calledPath = "";
    const client = {
      get: async (path: string) => {
        calledPath = path;
        return {
          data: {
            default_branch: "",
            branches: [
              { name: "", head_sha: "abc123def456", last_indexed_at: "2026-06-01T09:00:00Z" },
            ],
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    const refs = await loadRepoBranches(client, "repo-1");

    expect(calledPath).toBe("/api/v0/repositories/repo-1/branches");
    expect(refs).toEqual({
      defaultBranch: "",
      branches: [{ name: "", headSha: "abc123def456", lastIndexedAt: "2026-06-01T09:00:00Z" }],
    });
  });

  it("follows next_cursor across pages so the branch selector gets all branches", async () => {
    const calledPaths: string[] = [];
    const client = {
      get: async (path: string) => {
        calledPaths.push(path);
        if (!path.includes("cursor=")) {
          return {
            data: {
              default_branch: "main",
              branches: [
                { name: "main", head_sha: "sha-main", last_indexed_at: "2026-06-01T09:00:00Z" },
              ],
              truncated: true,
              next_cursor: "page-2-cursor",
            },
            error: null,
            truth: null,
          };
        }
        return {
          data: {
            default_branch: "main",
            branches: [
              { name: "release", head_sha: "sha-release", last_indexed_at: "2026-06-01T09:00:00Z" },
            ],
            truncated: false,
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    const refs = await loadRepoBranches(client, "repo-1");

    expect(calledPaths).toEqual([
      "/api/v0/repositories/repo-1/branches",
      "/api/v0/repositories/repo-1/branches?cursor=page-2-cursor",
    ]);
    expect(refs).toEqual({
      defaultBranch: "main",
      branches: [
        { name: "main", headSha: "sha-main", lastIndexedAt: "2026-06-01T09:00:00Z" },
        { name: "release", headSha: "sha-release", lastIndexedAt: "2026-06-01T09:00:00Z" },
      ],
    });
  });

  it("stops paging branches at the bounded page cap even if the server never stops truncating", async () => {
    let calls = 0;
    const client = {
      get: async () => {
        calls++;
        return {
          data: {
            default_branch: "main",
            branches: [{ name: `branch-${calls}`, head_sha: `sha-${calls}` }],
            truncated: true,
            next_cursor: `cursor-${calls}`,
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    const refs = await loadRepoBranches(client, "repo-1");

    expect(calls).toBe(10);
    expect(refs.branches).toHaveLength(10);
  });

  it("maps tree entries (dir child_count + file size/language) and the ref", async () => {
    let calledPath = "";
    const client = {
      get: async (path: string) => {
        calledPath = path;
        return {
          data: {
            ref: "abc123def4",
            path: "cmd",
            truncated: false,
            entries: [
              { name: "app", type: "dir", path: "cmd/app", child_count: 2 },
              { name: "main.go", type: "file", path: "cmd/app/main.go", size: 50, language: "go" },
            ],
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;
    const tree = await loadRepoTree(client, "repo-1", "cmd");
    expect(calledPath).toBe("/api/v0/repositories/repo-1/tree?path=cmd");
    expect(tree.ref).toBe("abc123def4");
    const dir = tree.entries.find((e) => e.type === "dir");
    expect(dir).toMatchObject({ name: "app", childCount: 2 });
    const f = tree.entries.find((e) => e.type === "file");
    expect(f).toMatchObject({ name: "main.go", size: 50, language: "go" });
  });

  it("passes selected refs through tree and content requests", async () => {
    const calledPaths: string[] = [];
    const client = {
      get: async (path: string) => {
        calledPaths.push(path);
        if (path.includes("/tree")) {
          return { data: { ref: "abc123", path: "cmd", entries: [] }, error: null, truth: null };
        }
        return {
          data: {
            path: "README.md",
            ref: "abc123",
            encoding: "utf-8",
            content: "# Hi\n",
            size: 5,
            truncated: false,
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    await loadRepoTree(client, "repo-1", "cmd", "main");
    await loadRepoFile(client, "repo-1", "README.md", "main");

    expect(calledPaths).toEqual([
      "/api/v0/repositories/repo-1/tree?path=cmd&ref=main",
      "/api/v0/repositories/repo-1/content?path=README.md&ref=main",
    ]);
  });

  it("appends language= to the tree request when a language filter is provided", async () => {
    let calledPath = "";
    const client = {
      get: async (path: string) => {
        calledPath = path;
        return { data: { ref: "abc123", path: "", entries: [] }, error: null, truth: null };
      },
    } as unknown as EshuApiClient;

    await loadRepoTree(client, "repo-1", "", "main", "go");

    expect(calledPath).toBe("/api/v0/repositories/repo-1/tree?ref=main&language=go");
  });

  it("omits language= from the tree request when language is empty", async () => {
    let calledPath = "";
    const client = {
      get: async (path: string) => {
        calledPath = path;
        return { data: { ref: "abc123", path: "", entries: [] }, error: null, truth: null };
      },
    } as unknown as EshuApiClient;

    await loadRepoTree(client, "repo-1", "", "", "");

    expect(calledPath).toBe("/api/v0/repositories/repo-1/tree");
  });

  it("propagates tree error envelopes instead of rendering an empty tree", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_runtime_profile",
          message: "repository tree unavailable",
          capability: "repository.tree",
        },
        truth: null,
      }),
    } as unknown as EshuApiClient;

    await expect(loadRepoTree(client, "repo-1")).rejects.toThrow("unsupported_runtime_profile");
  });

  it("loads utf-8 file content and decodes it as text", async () => {
    const client = {
      get: async () => ({
        data: {
          path: "README.md",
          ref: "abc",
          encoding: "utf-8",
          content: "# Hi\n",
          size: 5,
          language: "markdown",
          truncated: false,
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;
    const file = await loadRepoFile(client, "repo-1", "README.md");
    expect(file.provenance).toBe("live");
    expect(decodeRepoFile(file)).toEqual({ text: "# Hi\n", binary: false });
  });

  it("flags base64 content as binary", async () => {
    const client = {
      get: async () => ({
        data: { path: "bin.dat", encoding: "base64", content: "//79", size: 3, truncated: false },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;
    const file = await loadRepoFile(client, "repo-1", "bin.dat");
    expect(decodeRepoFile(file).binary).toBe(true);
  });

  it("returns unavailable provenance when content errors", async () => {
    const client = {
      get: async () => {
        throw new Error("404");
      },
    } as unknown as EshuApiClient;
    const file = await loadRepoFile(client, "repo-1", "missing");
    expect(file.provenance).toBe("unavailable");
  });

  it("returns unavailable provenance when content returns an Eshu error envelope", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_runtime_profile",
          message: "repository content unavailable",
          capability: "repository.content",
        },
        truth: null,
      }),
    } as unknown as EshuApiClient;

    const file = await loadRepoFile(client, "repo-1", "README.md");

    expect(file.provenance).toBe("unavailable");
    expect(file.content).toBe("");
  });
});
