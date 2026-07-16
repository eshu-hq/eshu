import { describe, expect, it } from "vitest";

import { repositoryApiPathFromSourcePath } from "./repositoryRouteWorkflowProbe";
import { repositoryPathsFromSourceHref } from "./routeWorkflowProbes";

describe("repositoryPathsFromSourceHref", () => {
  it("derives repository API requests under the versioned API prefix", () => {
    expect(repositoryApiPathFromSourcePath("/repositories/repository%3Ar_123/source")).toBe(
      "/api/v0/repositories/repository%3Ar_123",
    );
  });

  it("derives source and workspace routes from the same retained repository id", () => {
    expect(repositoryPathsFromSourceHref("/repositories/repository%3Ar_123/source")).toEqual({
      sourcePath: "/repositories/repository%3Ar_123/source",
      workspacePath: "/workspace/repositories/repository%3Ar_123",
    });
  });

  it("rejects a generic or malformed route instead of inventing an id", () => {
    expect(repositoryPathsFromSourceHref("/repositories/source")).toBeNull();
    expect(repositoryPathsFromSourceHref("/workspace/repositories/repository%3Ar_123")).toBeNull();
  });
});
