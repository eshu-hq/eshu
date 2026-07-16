import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { DeadCodePage } from "./DeadCodePage";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

describe("DeadCodePage repository scoping", () => {
  it("counts unresolved repositories by canonical id instead of display label", () => {
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
          filePath: "server/first.ts",
          repoId: "repository:r_11111111",
        },
        {
          id: "dead-2",
          type: "Dead code",
          entity: "unresolved repository",
          title: "Unreferenced symbol second",
          detail: "server/second.ts · unused",
          truth: "derived",
          filePath: "server/second.ts",
          repoId: "repository:r_22222222",
        },
      ],
    };

    render(
      <MemoryRouter>
        <DeadCodePage model={model} />
      </MemoryRouter>,
    );

    expect(screen.getByText("Repositories represented")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Show repository breakdown" })).toHaveTextContent(
      "2",
    );
    expect(screen.getAllByText("unresolved repository")).toHaveLength(2);
  });
});
