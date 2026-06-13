import { demoModel } from "./demoModel";

describe("demoModel", () => {
  it("marks fixture-backed snapshot sections as demo, not live", () => {
    expect(Object.values(demoModel.provenance).every((source) => source === "demo")).toBe(true);
    expect(demoModel.runtime.profile).toBe("demo_fixture");
  });
});
