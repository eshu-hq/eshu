import { repositorySearchDestination } from "./appSearchRouting";

describe("repositorySearchDestination", () => {
  it("preserves Code Graph destination intent for an exact repository search", () => {
    expect(repositorySearchDestination("/code-graph", "repository:r2")).toBe(
      "/code-graph?repo_id=repository%3Ar2",
    );
  });

  it("keeps repository source as the default exact-search destination", () => {
    expect(repositorySearchDestination("/findings", "repository:r2")).toBe(
      "/repositories/repository%3Ar2/source",
    );
  });
});
