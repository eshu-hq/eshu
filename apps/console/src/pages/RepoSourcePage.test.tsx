import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";

import { RepoSourcePage } from "./RepoSourcePage";
import type { EshuApiClient } from "../api/client";

describe("RepoSourcePage", () => {
  it("opens the requested source file from the path query parameter", async () => {
    const client = {
      get: async (path: string) => {
        if (path.includes("/content?")) {
          return {
            data: {
              path: "server/handlers/profile.ts",
              ref: "main",
              encoding: "utf-8",
              content: "line one\nexport function put() {}\nline three",
              size: 1,
              language: "typescript",
              truncated: false,
            },
            error: null,
            truth: null,
          };
        }
        return {
          data: {
            ref: "main",
            path: "server/handlers",
            entries: [
              {
                name: "profile.ts",
                type: "file",
                path: "server/handlers/profile.ts",
                size: 1,
                language: "typescript",
              },
            ],
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter
        initialEntries={[
          "/repositories/repository%3Ar_1/source?path=server%2Fhandlers%2Fprofile.ts&lineStart=2&lineEnd=2",
        ]}
      >
        <Routes>
          <Route path="/repositories/:id/source" element={<RepoSourcePage client={client} />} />
        </Routes>
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.getByText("server/handlers/profile.ts")).toBeInTheDocument());
    expect(screen.getByText(/export function put/)).toBeInTheDocument();
    expect(screen.getByTestId("source-line-2")).toHaveClass("is-highlighted");
  });

  it("labels raw repository-id source routes with the repository name", async () => {
    const client = {
      get: async (path: string) => {
        if (path === "/api/v0/repositories?limit=500&offset=0") {
          return {
            data: {
              repositories: [{ id: "repository:r_1", name: "svc-platform" }],
            },
            error: null,
            truth: null,
          };
        }
        return {
          data: {
            ref: "main",
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

    expect(await screen.findByRole("heading", { name: /svc-platform/ })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: /repository:r_1/ })).not.toBeInTheDocument();
  });

  // Branch-selector tests (indexed ref, source-backed choices, default-first
  // display sort, truncated-note partial state) moved to
  // RepoSourcePage.branches.test.tsx to keep this file under the 500-line cap.

  it("keeps file selections shareable by updating the source URL", async () => {
    const client = {
      get: async (path: string) => {
        if (path.includes("/content?")) {
          return {
            data: {
              path: "server/index.ts",
              ref: "main",
              encoding: "utf-8",
              content: "export const handler = true;",
              size: 1,
              language: "typescript",
              truncated: false,
            },
            error: null,
            truth: null,
          };
        }
        return {
          data: {
            ref: "main",
            path: "",
            entries: [
              {
                name: "index.ts",
                type: "file",
                path: "server/index.ts",
                size: 1,
                language: "typescript",
              },
            ],
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/repositories/repository%3Ar_1/source"]}>
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

    fireEvent.click(
      await screen.findByText(
        (_, element) =>
          element?.className === "t-name" && (element.textContent ?? "").includes("index.ts"),
      ),
    );

    await waitFor(() => {
      expect(screen.getByTestId("source-location")).toHaveTextContent(
        "/repositories/repository%3Ar_1/source?path=server%2Findex.ts",
      );
    });
    expect(screen.getByText(/export const handler/)).toBeInTheDocument();
  });
  it("shows the language dropdown populated from repo-wide stats even when tree entries are only directories", async () => {
    // At the repo root the tree contains only directories — the old client-side
    // filter built options from tree.entries and came up empty. The fix sources
    // options from GET /stats so the dropdown is populated regardless.
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        if (path.includes("/stats")) {
          return {
            data: { languages: ["typescript", "go", "python"] },
            error: null,
            truth: null,
          };
        }
        // Root tree: all directories, no files
        return {
          data: {
            ref: "main",
            path: "",
            entries: [
              { name: "cmd", type: "dir", path: "cmd", child_count: 3 },
              { name: "internal", type: "dir", path: "internal", child_count: 12 },
            ],
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

    // Wait for tree to load (directories appear) and language dropdown to render
    // from repo-wide stats. findByRole waits for the element to appear.
    const select = await screen.findByRole("combobox", { name: "Filter files by language" });
    expect(select).toBeTruthy();

    // Options must be sorted repo-wide languages (go < python < typescript)
    const options = Array.from(select.querySelectorAll("option")).map((o) => o.textContent);
    expect(options).toEqual(["All", "go", "python", "typescript"]);

    // stats endpoint was called for the repo
    expect(calls.some((c) => c.includes("/stats"))).toBe(true);
  });

  it("drives a server-side language filter and reflects it in the URL when a language is selected", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        if (path.includes("/stats")) {
          return {
            data: { languages: ["go", "typescript"] },
            error: null,
            truth: null,
          };
        }
        // Return only go files when language=go is requested
        if (path.includes("language=go")) {
          return {
            data: {
              ref: "main",
              path: "",
              entries: [
                { name: "main.go", type: "file", path: "main.go", size: 10, language: "go" },
              ],
            },
            error: null,
            truth: null,
          };
        }
        return {
          data: {
            ref: "main",
            path: "",
            entries: [
              { name: "main.go", type: "file", path: "main.go", size: 10, language: "go" },
              { name: "index.ts", type: "file", path: "index.ts", size: 5, language: "typescript" },
            ],
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/repositories/repository%3Ar_1/source"]}>
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

    // Wait for language dropdown to appear
    const select = await screen.findByRole("combobox", { name: "Filter files by language" });

    // Select "go"
    fireEvent.change(select, { target: { value: "go" } });

    // URL must reflect the language selection
    await waitFor(() => {
      const loc = screen.getByTestId("source-location").textContent ?? "";
      expect(loc).toContain("language=go");
    });

    // The tree must have been re-fetched with language=go in the query
    await waitFor(() => {
      expect(calls.some((c) => c.includes("language=go"))).toBe(true);
    });
  });

  it("restores the language filter from the URL on load", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        if (path.includes("/stats")) {
          return { data: { languages: ["go", "typescript"] }, error: null, truth: null };
        }
        return {
          data: {
            ref: "main",
            path: "",
            entries: [{ name: "main.go", type: "file", path: "main.go", size: 10, language: "go" }],
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/repositories/repository%3Ar_1/source?language=go"]}>
        <Routes>
          <Route path="/repositories/:id/source" element={<RepoSourcePage client={client} />} />
        </Routes>
      </MemoryRouter>,
    );

    // Dropdown should reflect the URL-sourced language value
    const select = await screen.findByRole("combobox", { name: "Filter files by language" });
    expect((select as HTMLSelectElement).value).toBe("go");

    // The initial tree fetch must include language=go
    await waitFor(() => {
      expect(calls.some((c) => c.includes("language=go"))).toBe(true);
    });
  });

  it("preserves the language filter in the URL when opening a file from the tree", async () => {
    const client = {
      get: async (path: string) => {
        if (path.includes("/stats")) {
          return { data: { languages: ["go", "typescript"] }, error: null, truth: null };
        }
        if (path.includes("/content")) {
          return {
            data: { path: "main.go", ref: "main", content: "package main", size: 10 },
            error: null,
            truth: null,
          };
        }
        return {
          data: {
            ref: "main",
            path: "",
            entries: [{ name: "main.go", type: "file", path: "main.go", size: 10, language: "go" }],
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/repositories/repository%3Ar_1/source?language=go"]}>
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

    // Open the file from the tree.
    fireEvent.click(
      await screen.findByText(
        (_, element) =>
          element?.className === "t-name" && (element.textContent ?? "").includes("main.go"),
      ),
    );

    // The language scope must survive the navigation to the file view.
    await waitFor(() => {
      const loc = screen.getByTestId("source-location").textContent ?? "";
      expect(loc).toContain("language=go");
    });
  });
});

function LocationProbe(): React.JSX.Element {
  const location = useLocation();
  return <output data-testid="source-location">{location.pathname + location.search}</output>;
}
