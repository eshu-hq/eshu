// RepoSourcePage.branches.test.tsx
// Branch-selector-focused tests split out of RepoSourcePage.test.tsx to keep
// that file under the repo's 500-line cap (#5503 T1/T2/UX: bounded paging +
// default-on-top display sort + the "branch list truncated" partial-state
// note).
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";

import { RepoSourcePage } from "./RepoSourcePage";
import type { EshuApiClient } from "../api/client";

describe("RepoSourcePage branch selector", () => {
  it("shows the indexed repository ref returned by the branches endpoint", async () => {
    const client = {
      get: async (path: string) => {
        if (path.startsWith("/api/v0/repositories/repository%3Ar_1/branches")) {
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
        }
        return {
          data: {
            ref: "abc123def456",
            path: "",
            entries: [],
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/repositories/repository%3Ar_1/source"]}>
        <Routes>
          <Route path="/repositories/:id/source" element={<RepoSourcePage client={client} />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByText("Indexed ref")).toBeInTheDocument();
    expect(screen.getByText("abc123def4")).toBeInTheDocument();
    expect(screen.queryByText(/Branch selection is pending/)).not.toBeInTheDocument();
  });

  it("renders source-backed branch choices and keeps selected refs in source reads", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        if (path.startsWith("/api/v0/repositories/repository%3Ar_1/branches")) {
          return {
            data: {
              default_branch: "main",
              branches: [
                {
                  name: "main",
                  kind: "branch",
                  head_sha: "abc123def456",
                  is_default: true,
                  last_indexed_at: "2026-06-01T09:00:00Z",
                },
                {
                  name: "release",
                  kind: "branch",
                  head_sha: "def456abc123",
                  is_default: false,
                  last_indexed_at: "2026-06-01T10:00:00Z",
                },
              ],
              truncated: false,
            },
            error: null,
            truth: null,
          };
        }
        if (path.includes("/content?")) {
          return {
            data: {
              path: "README.md",
              ref: path.includes("ref=release") ? "def456abc123" : "abc123def456",
              encoding: "utf-8",
              content: "selected branch content",
              size: 1,
              language: "markdown",
              truncated: false,
            },
            error: null,
            truth: null,
          };
        }
        return {
          data: {
            ref: path.includes("ref=release") ? "def456abc123" : "abc123def456",
            path: "",
            entries: [
              { name: "README.md", type: "file", path: "README.md", size: 1, language: "markdown" },
            ],
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter
        initialEntries={["/repositories/repository%3Ar_1/source?path=README.md&ref=main"]}
      >
        <Routes>
          <Route
            path="/repositories/:id/source"
            element={
              <>
                <RepoSourcePage client={client} />
                <LocationProbe />
              </>
            }
          />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByRole("combobox", { name: "Repository ref" })).toHaveValue("main");
    await waitFor(() => {
      expect(calls).toContain("/api/v0/repositories/repository%3Ar_1/tree?ref=main");
      expect(calls).toContain(
        "/api/v0/repositories/repository%3Ar_1/content?path=README.md&ref=main",
      );
    });

    fireEvent.change(screen.getByRole("combobox", { name: "Repository ref" }), {
      target: { value: "release" },
    });

    await waitFor(() => {
      expect(screen.getByTestId("source-location")).toHaveTextContent(
        "/repositories/repository%3Ar_1/source?path=README.md&ref=release",
      );
      expect(calls).toContain("/api/v0/repositories/repository%3Ar_1/tree?ref=release");
      expect(calls).toContain(
        "/api/v0/repositories/repository%3Ar_1/content?path=README.md&ref=release",
      );
    });
  });

  it("pins the default branch to the top of the dropdown even though the server returns branches alphabetically (locked UX decision)", async () => {
    const client = {
      get: async (path: string) => {
        if (path.startsWith("/api/v0/repositories/repository%3Ar_1/branches")) {
          // Server order is alphabetical (#5503): "alpha" sorts before the
          // default branch "zeta". The dropdown must still show "zeta" first.
          return {
            data: {
              default_branch: "zeta",
              branches: [
                { name: "alpha", kind: "branch", head_sha: "sha-alpha", is_default: false },
                { name: "mid", kind: "branch", head_sha: "sha-mid", is_default: false },
                { name: "zeta", kind: "branch", head_sha: "sha-zeta", is_default: true },
              ],
              truncated: false,
            },
            error: null,
            truth: null,
          };
        }
        return { data: { ref: "sha-zeta", path: "", entries: [] }, error: null, truth: null };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/repositories/repository%3Ar_1/source"]}>
        <Routes>
          <Route path="/repositories/:id/source" element={<RepoSourcePage client={client} />} />
        </Routes>
      </MemoryRouter>,
    );

    const select = await screen.findByRole("combobox", { name: "Repository ref" });
    const optionLabels = Array.from(select.querySelectorAll("option")).map(
      (option) => option.textContent,
    );
    expect(optionLabels).toEqual([
      expect.stringContaining("zeta"),
      expect.stringContaining("alpha"),
      expect.stringContaining("mid"),
    ]);
    expect(screen.queryByText("branch list truncated")).not.toBeInTheDocument();
  });

  it("shows a truncated note when the branch list did not reach completeness", async () => {
    let branchCalls = 0;
    const client = {
      get: async (path: string) => {
        if (path.startsWith("/api/v0/repositories/repository%3Ar_1/branches")) {
          branchCalls++;
          return {
            data: {
              default_branch: "main",
              branches: [
                { name: `branch-${branchCalls}`, kind: "branch", head_sha: `sha-${branchCalls}` },
              ],
              truncated: true,
              next_cursor: `cursor-${branchCalls}`,
            },
            error: null,
            truth: null,
          };
        }
        return { data: { ref: "sha-1", path: "", entries: [] }, error: null, truth: null };
      },
    } as unknown as EshuApiClient;

    // A response that stays truncated:true forever exhausts the client's
    // sanity cap (MAX_REPO_BRANCH_PAGES), so loadRepoBranches resolves with
    // complete:false and the page must surface that partial state.
    render(
      <MemoryRouter initialEntries={["/repositories/repository%3Ar_1/source"]}>
        <Routes>
          <Route path="/repositories/:id/source" element={<RepoSourcePage client={client} />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByText("branch list truncated")).toBeInTheDocument();
  });
});

function LocationProbe(): React.JSX.Element {
  const location = useLocation();
  return <output data-testid="source-location">{location.pathname + location.search}</output>;
}
