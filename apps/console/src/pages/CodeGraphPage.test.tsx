import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { CodeGraphPage } from "./CodeGraphPage";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

describe("CodeGraphPage", () => {
  it("renders the demo-style code analyzer from live code relationships", async () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        {
          id: "dead-1",
          type: "Dead code",
          entity: "svc-platform",
          title: "Unreferenced symbol post",
          detail: "server/handlers/install.ts · unused",
          truth: "derived",
          entityId: "content-entity:e1",
          filePath: "server/handlers/install.ts",
          startLine: 17,
          endLine: 54,
          language: "typescript",
          labels: ["Function"],
          classification: "unused"
        }
      ]
    };
    const client = {
      post: async () => ({
        data: {
          entity_id: "content-entity:e1",
          name: "post",
          labels: ["Function"],
          relationships: [
            { direction: "incoming", type: "CALLS", source_id: "content-entity:e2", source_name: "handler" },
            { direction: "outgoing", type: "IMPORTS", target_id: "content-entity:e3", target_name: "installService" }
          ]
        },
        error: null,
        truth: null
      })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={model} client={client} />
      </MemoryRouter>
    );

    expect(screen.getByRole("heading", { name: "Code graph" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Analyzer")).toBeInTheDocument());
    expect(screen.getByText("Import edges")).toBeInTheDocument();
    expect(screen.getByText("Call edges")).toBeInTheDocument();
    expect(screen.getByText("Dead in this repo · 1")).toBeInTheDocument();
    expect(screen.getAllByText("post").length).toBeGreaterThan(0);
  });

  it("uses the resolved repository name in the candidate selector", async () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        {
          id: "dead-1",
          type: "Dead code",
          entity: "svc-platform",
          title: "Unreferenced symbol post",
          detail: "server/handlers/install.ts · unused",
          truth: "derived",
          entityId: "content-entity:e1",
          filePath: "server/handlers/install.ts",
          classification: "unused"
        }
      ]
    };

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={model} />
      </MemoryRouter>
    );

    const selector = screen.getByRole("combobox");
    expect(selector).toHaveTextContent("post · svc-platform");
    expect(selector).not.toHaveTextContent("repository:r_");
  });

  it("turns clicked graph dead-code nodes into actionable evidence", async () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        {
          id: "dead-1",
          type: "Dead code",
          entity: "svc-platform",
          title: "Unreferenced symbol post",
          detail: "server/handlers/install.ts · unused",
          truth: "derived",
          entityId: "content-entity:e1",
          filePath: "server/handlers/install.ts",
          startLine: 17,
          endLine: 54,
          language: "typescript",
          labels: ["Function"],
          classification: "unused",
          repoId: "repository:r_1"
        },
        {
          id: "dead-2",
          type: "Dead code",
          entity: "svc-platform",
          title: "Unreferenced symbol put",
          detail: "server/handlers/profile.ts · unused",
          truth: "derived",
          entityId: "content-entity:e2",
          filePath: "server/handlers/profile.ts",
          startLine: 41,
          endLine: 75,
          language: "typescript",
          labels: ["Function"],
          classification: "unused",
          repoId: "repository:r_1"
        }
      ]
    };

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={model} />
      </MemoryRouter>
    );

    const putNode = screen.getAllByText("put").find((node) => node.tagName.toLowerCase() === "text");
    expect(putNode).toBeDefined();
    fireEvent.click(putNode!);

    expect(screen.getByText("Selected symbol")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "server/handlers/profile.ts:41-75" })).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar_1/source?path=server%2Fhandlers%2Fprofile.ts&lineStart=41&lineEnd=75"
    );
    expect(screen.getByRole("link", { name: "Open source" })).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar_1/source?path=server%2Fhandlers%2Fprofile.ts&lineStart=41&lineEnd=75"
    );
    expect(screen.getByRole("link", { name: "Explore repo graph" })).toHaveAttribute(
      "href",
      "/explorer?q=svc-platform"
    );
  });

  it("explains when related symbols are not source-linkable", async () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        {
          id: "dead-1",
          type: "Dead code",
          entity: "svc-platform",
          title: "Unreferenced symbol post",
          detail: "server/handlers/install.ts · unused",
          truth: "derived",
          entityId: "content-entity:e1",
          filePath: "server/handlers/install.ts",
          startLine: 17,
          language: "typescript",
          classification: "unused",
          repoId: "repository:r_platform"
        }
      ]
    };
    const client = {
      post: async () => ({
        data: {
          entity_id: "content-entity:e1",
          name: "post",
          labels: ["Function"],
          relationships: [{ direction: "incoming", type: "CALLS", source_id: "content-entity:e2", source_name: "caller" }]
        },
        error: null,
        truth: null
      })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={model} client={client} />
      </MemoryRouter>
    );

    const callerNode = await screen.findByText("caller");
    fireEvent.click(callerNode);

    expect(screen.getByText("Related symbol source metadata unavailable from POST /api/v0/code/relationships/story.")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Open source" })).not.toBeInTheDocument();
  });

  it("turns dead-code rows in the analyzer into selectable source evidence", async () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        {
          id: "dead-1",
          type: "Dead code",
          entity: "svc-platform",
          title: "Unreferenced symbol post",
          detail: "server/handlers/install.ts · unused",
          truth: "derived",
          entityId: "content-entity:e1",
          filePath: "server/handlers/install.ts",
          startLine: 17,
          endLine: 54,
          language: "typescript",
          labels: ["Function"],
          classification: "unused",
          repoId: "repository:r_1"
        },
        {
          id: "dead-2",
          type: "Dead code",
          entity: "svc-platform",
          title: "Unreferenced symbol put",
          detail: "server/handlers/profile.ts · unused",
          truth: "derived",
          entityId: "content-entity:e2",
          filePath: "server/handlers/profile.ts",
          startLine: 41,
          endLine: 75,
          language: "typescript",
          labels: ["Function"],
          classification: "unused",
          repoId: "repository:r_1"
        }
      ]
    };

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={model} />
      </MemoryRouter>
    );

    fireEvent.click(screen.getByRole("button", { name: "put unused" }));

    expect(screen.getByRole("link", { name: "server/handlers/profile.ts:41-75" })).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar_1/source?path=server%2Fhandlers%2Fprofile.ts&lineStart=41&lineEnd=75"
    );
    expect(screen.getByRole("combobox")).toHaveValue("dead-2");
  });

  it("selects a dead-code candidate from the code-graph URL parameter", async () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        {
          id: "dead-1",
          type: "Dead code",
          entity: "svc-platform",
          title: "Unreferenced symbol post",
          detail: "server/handlers/install.ts · unused",
          truth: "derived",
          entityId: "content-entity:e1",
          filePath: "server/handlers/install.ts",
          startLine: 17,
          endLine: 54,
          labels: ["Function"],
          classification: "unused",
          repoId: "repository:r_1"
        },
        {
          id: "dead-2",
          type: "Dead code",
          entity: "svc-platform",
          title: "Unreferenced symbol put",
          detail: "server/handlers/profile.ts · unused",
          truth: "derived",
          entityId: "content-entity:e2",
          filePath: "server/handlers/profile.ts",
          startLine: 41,
          endLine: 75,
          labels: ["Function"],
          classification: "unused",
          repoId: "repository:r_1"
        }
      ]
    };

    render(
      <MemoryRouter initialEntries={["/code-graph?candidate=dead-2"]}>
        <CodeGraphPage model={model} />
      </MemoryRouter>
    );

    expect(screen.getByRole("combobox")).toHaveValue("dead-2");
    expect(screen.getByRole("link", { name: "server/handlers/profile.ts:41-75" })).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar_1/source?path=server%2Fhandlers%2Fprofile.ts&lineStart=41&lineEnd=75"
    );
  });

  it("loads live dead-code candidates and uses the documented relationships depth field", async () => {
    const calls: { readonly path: string; readonly body: unknown }[] = [];
    const client = {
      get: async () => ({
        data: { repositories: [{ id: "repository:r1", name: "svc-platform" }] },
        error: null,
        truth: null
      }),
      post: async (path: string, body: unknown) => {
        calls.push({ path, body });
        if (path === "/api/v0/code/dead-code") {
          return {
            data: {
              limit: 100,
              results: [{
                classification: "unused",
                entity_id: "content-entity:e1",
                file_path: "server/routes.ts",
                labels: ["Function"],
                language: "typescript",
                name: "unusedRoute",
                repo_id: "repository:r1",
                start_line: 10
              }],
              truncated: false
            },
            error: null,
            truth: { level: "derived", freshness: { state: "fresh" }, profile: "production" }
          };
        }
        return {
          data: {
            entity_id: "content-entity:e1",
            name: "unusedRoute",
            labels: ["Function"],
            relationships: []
          },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={{ ...demoModel, source: "live", findings: [] }} client={client} />
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByRole("combobox")).toHaveTextContent("unusedRoute · svc-platform"));
    expect(screen.queryByText("repository:r1")).not.toBeInTheDocument();
    expect(calls).toContainEqual({ path: "/api/v0/code/dead-code", body: { limit: 100 } });
    await waitFor(() => expect(calls).toContainEqual({
      path: "/api/v0/code/relationships/story",
      body: {
        entity_id: "content-entity:e1",
        direction: "both",
        relationship_types: ["CALLS", "IMPORTS", "REFERENCES", "INHERITS", "OVERRIDES", "TAINT_FLOWS_TO"],
        limit: 50
      }
    }));
  });

  it("reports live candidate load failures instead of hiding the degraded source", async () => {
    const client = {
      get: async () => {
        throw new Error("repository catalog unavailable");
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={{ ...demoModel, source: "live" }} client={client} />
      </MemoryRouter>
    );

    await waitFor(() => expect(screen.getByText(/Failed to load live dead-code candidates/)).toBeInTheDocument());
  });

  it("surfaces Eshu error envelopes from live code relationships", async () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        {
          id: "dead-1",
          type: "Dead code",
          entity: "svc-platform",
          title: "Unreferenced symbol post",
          detail: "server/handlers/install.ts · unused",
          truth: "derived",
          entityId: "content-entity:e1",
          filePath: "server/handlers/install.ts"
        }
      ]
    };
    const client = {
      post: async () => ({
        data: null,
        error: {
          code: "unsupported_capability",
          message: "code relationships unavailable"
        },
        truth: null
      })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/code-graph"]}>
        <CodeGraphPage model={model} client={client} />
      </MemoryRouter>
    );

    await waitFor(() =>
      expect(screen.getByText("unsupported_capability: code relationships unavailable")).toBeInTheDocument()
    );
  });
});
