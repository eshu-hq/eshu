import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { CodeGraphPage } from "./CodeGraphPage";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

describe("CodeGraphPage repository scoping", () => {
  it("does not group different repositories that share an unresolved display label", () => {
    const model: ConsoleModel = {
      ...demoModel,
      findings: [
        {
          id: "dead-1",
          type: "Dead code",
          entity: "unresolved repository",
          title: "Unreferenced symbol first",
          detail: "server/first.ts · unused",
          truth: "derived",
          entityId: "content-entity:first",
          filePath: "server/first.ts",
          repoId: "repository:r_11111111"
        },
        {
          id: "dead-2",
          type: "Dead code",
          entity: "unresolved repository",
          title: "Unreferenced symbol second",
          detail: "server/second.ts · unused",
          truth: "derived",
          entityId: "content-entity:second",
          filePath: "server/second.ts",
          repoId: "repository:r_22222222"
        }
      ]
    };

    render(
      <MemoryRouter initialEntries={["/code-graph?candidate=dead-1"]}>
        <CodeGraphPage model={model} />
      </MemoryRouter>
    );

    expect(screen.getByText("Dead in this repo · 1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "first candidate" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "second candidate" })).not.toBeInTheDocument();
  });
});
