import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

function repoRoot(): string {
  return process.cwd().endsWith("apps/console") ? resolve(process.cwd(), "../..") : process.cwd();
}

function repoFile(path: string): string {
  return readFileSync(resolve(repoRoot(), path), "utf8");
}

describe("prototype shell parity", () => {
  it("keeps the live repositories nav count source-backed by repository inventory", () => {
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");

    expect(app).toContain("function repositoryNavCount(model)");
    expect(app).toContain("runtime.repositories");
    expect(app).toContain("runtime.repos");
    expect(app).toContain('source.mode === "live" ? liveConsoleData(ESHU, source.live) : ESHU');
    expect(app).toContain('{ id: "repos", label: "Repositories", icon: "catalog", count: repositoryNavCount }');
    expect(app).not.toContain('count: (m) => m.services.filter((s) => s.repo).length');
  });

  it("keeps the standalone repositories route deriving names from repo slugs", () => {
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-repositories-parity.jsx");

    expect(page).toContain("function repoDisplayName(repo)");
    expect(page).toContain("repoSlugLeaf(repoText(repo.repo_slug))");
    expect(page).toContain("repoDisplayName(repo)");
    expect(page).not.toContain("name: repoText(repo.name) || repoText(repo.id)");
  });
});
