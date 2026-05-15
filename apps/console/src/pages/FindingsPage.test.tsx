import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";
import { FindingsPage } from "./FindingsPage";

describe("FindingsPage", () => {
  it("starts findings with live dead-code results", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) =>
        new Request(input).url.endsWith("/api/v0/repositories")
          ? Response.json({
              repositories: [
                {
                  id: "repository:r_1",
                  name: "boats-chatgpt-app"
                }
              ]
            })
          : Response.json({
              data: {
                results: [
                  {
                    classification: "unused",
                    file_path: "server/src/api/boatsClient.ts",
                    name: "parseRange",
                    repo_id: "repository:r_1"
                  },
                  {
                    classification: "unused",
                    file_path: "src/intake/base.ts",
                    name: "getList",
                    repo_name: "mobius-tools"
                  }
                ]
              },
              error: null,
              truth: {
                capability: "code_quality.dead_code",
                freshness: { state: "fresh" },
                level: "derived",
                profile: "local_authoritative"
              }
            })
      )
    );

    render(
      <MemoryRouter>
        <FindingsPage />
      </MemoryRouter>
    );

    expect(screen.getByRole("heading", { name: "Findings" })).toBeInTheDocument();
    expect(await screen.findAllByText("Dead code")).toHaveLength(2);
    expect(screen.getByText("parseRange")).toBeInTheDocument();
    expect(screen.getAllByText("derived")).toHaveLength(2);
    expect(screen.getByText("2 findings")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Search findings"), {
      target: { value: "mobius" }
    });

    expect(screen.queryByText("parseRange")).not.toBeInTheDocument();
    expect(screen.getByText("getList")).toBeInTheDocument();
  });
});
