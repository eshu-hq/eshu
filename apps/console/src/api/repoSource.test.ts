import { describe, expect, it } from "vitest";
import { decodeRepoFile, loadRepoFile, loadRepoTree } from "./repoSource";
import type { EshuApiClient } from "./client";

describe("repoSource", () => {
  it("maps tree entries (dir child_count + file size/language) and the ref", async () => {
    let calledPath = "";
    const client = {
      get: async (path: string) => { calledPath = path; return {
        data: { ref: "abc123def4", path: "cmd", truncated: false, entries: [
          { name: "app", type: "dir", path: "cmd/app", child_count: 2 },
          { name: "main.go", type: "file", path: "cmd/app/main.go", size: 50, language: "go" }
        ] }, error: null, truth: null
      }; }
    } as unknown as EshuApiClient;
    const tree = await loadRepoTree(client, "repo-1", "cmd");
    expect(calledPath).toBe("/api/v0/repositories/repo-1/tree?path=cmd");
    expect(tree.ref).toBe("abc123def4");
    const dir = tree.entries.find((e) => e.type === "dir");
    expect(dir).toMatchObject({ name: "app", childCount: 2 });
    const f = tree.entries.find((e) => e.type === "file");
    expect(f).toMatchObject({ name: "main.go", size: 50, language: "go" });
  });

  it("loads utf-8 file content and decodes it as text", async () => {
    const client = { get: async () => ({ data: { path: "README.md", ref: "abc", encoding: "utf-8", content: "# Hi\n", size: 5, language: "markdown", truncated: false }, error: null, truth: null }) } as unknown as EshuApiClient;
    const file = await loadRepoFile(client, "repo-1", "README.md");
    expect(file.provenance).toBe("live");
    expect(decodeRepoFile(file)).toEqual({ text: "# Hi\n", binary: false });
  });

  it("flags base64 content as binary", async () => {
    const client = { get: async () => ({ data: { path: "bin.dat", encoding: "base64", content: "//79", size: 3, truncated: false }, error: null, truth: null }) } as unknown as EshuApiClient;
    const file = await loadRepoFile(client, "repo-1", "bin.dat");
    expect(decodeRepoFile(file).binary).toBe(true);
  });

  it("returns unavailable provenance when content errors", async () => {
    const client = { get: async () => { throw new Error("404"); } } as unknown as EshuApiClient;
    const file = await loadRepoFile(client, "repo-1", "missing");
    expect(file.provenance).toBe("unavailable");
  });
});
