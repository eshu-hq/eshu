import { render, screen } from "@testing-library/react";

import { ImpactSelectedEdges } from "./ImpactSelectedEdges";

describe("ImpactSelectedEdges", () => {
  it("uses graph labels and a human relationship verb before canonical identities", () => {
    render(
      <ImpactSelectedEdges
        edges={[
          {
            layer: "deploy",
            s: "repository:r_config",
            t: "repository:r_catalog",
            verb: "DEPLOYS_FROM",
          },
        ]}
        nodes={[
          { col: 0, id: "repository:r_config", kind: "repo", label: "deployment-config" },
          { col: 1, id: "repository:r_catalog", kind: "repo", label: "catalog-api" },
        ]}
        selectedID="repository:r_config"
      />,
    );

    expect(
      screen.getByRole("listitem", { name: "deployment-config deploys from catalog-api" }),
    ).toBeInTheDocument();
    expect(screen.getByText("repository:r_config → repository:r_catalog")).toBeInTheDocument();
  });
});
