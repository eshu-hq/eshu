import { render, screen } from "@testing-library/react";
import { OperationsPage } from "./OperationsPage";
import { demoModel } from "../console/demoModel";

describe("OperationsPage", () => {
  it("labels repository language inventory with the aggregate endpoint", () => {
    render(<OperationsPage model={demoModel} />);

    expect(screen.getByText("GET /api/v0/repositories/language-inventory")).toBeInTheDocument();
    expect(screen.queryByText("GET /api/v0/repositories/by-language")).not.toBeInTheDocument();
  });

  it("renders live query latency series when metrics samples are available", () => {
    render(<OperationsPage model={{
      ...demoModel,
      series: {
        ...demoModel.series,
        deadLetters: [0, 1],
        queryP50: [3],
        queryP95: [7],
        queryP99: [11]
      }
    }} />);

    expect(screen.getByText("Query latency")).toBeInTheDocument();
    expect(screen.getByText("p50 3ms · p95 7ms · p99 11ms")).toBeInTheDocument();
  });

  it("renders live graph growth series when metrics samples are available", () => {
    render(<OperationsPage model={{
      ...demoModel,
      series: {
        ...demoModel.series,
        graphNodes: [41000, 41120],
        graphEdges: [128000, 129200]
      }
    }} />);

    expect(screen.getByText("Graph growth")).toBeInTheDocument();
    expect(screen.getByText("41.1k nodes · 129.2k edges")).toBeInTheDocument();
  });
});
