import { fireEvent, render, screen } from "@testing-library/react";
import { SearchCommand } from "./SearchCommand";
import { demoSearchCandidates } from "../api/mockData";

describe("SearchCommand", () => {
  it("filters repository and service candidates and reports the selected entity", () => {
    const selected: string[] = [];

    render(
      <SearchCommand
        candidates={demoSearchCandidates}
        onSelect={(candidate) => selected.push(candidate.id)}
      />
    );

    fireEvent.change(screen.getByLabelText("Search Eshu"), {
      target: { value: "checkout" }
    });
    fireEvent.click(screen.getByRole("button", { name: /checkout-service/i }));

    expect(selected).toEqual(["workload:checkout-service"]);
  });
});
