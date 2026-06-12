import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import { RepoSourcePage } from "./RepoSourcePage";

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
              truncated: false
            },
            error: null,
            truth: null
          };
        }
        return {
          data: {
            ref: "main",
            path: "server/handlers",
            entries: [{ name: "profile.ts", type: "file", path: "server/handlers/profile.ts", size: 1, language: "typescript" }]
          },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/repositories/repository%3Ar_1/source?path=server%2Fhandlers%2Fprofile.ts&lineStart=2&lineEnd=2"]}>
        <Routes>
          <Route path="/repositories/:id/source" element={<RepoSourcePage client={client} />} />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText("server/handlers/profile.ts")).toBeInTheDocument());
    expect(screen.getByText(/export function put/)).toBeInTheDocument();
    expect(screen.getByTestId("source-line-2")).toHaveClass("is-highlighted");
  });
});
