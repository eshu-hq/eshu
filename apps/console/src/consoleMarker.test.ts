import { consoleMarker } from "./consoleMarker";

describe("console package scaffold", () => {
  it("identifies the private read-only console app", () => {
    expect(consoleMarker).toEqual("eshu-console");
  });
});
