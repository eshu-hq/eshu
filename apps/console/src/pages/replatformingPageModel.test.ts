import type { ReplatformingFormState } from "./ReplatformingFilters";
import { hasAnchor } from "./replatformingPageModel";

const baseForm: ReplatformingFormState = {
  accountId: "",
  findingKinds: "",
  limit: "100",
  offset: "0",
  region: "",
  scopeId: "",
  scopeKind: "account",
};

describe("replatforming page model", () => {
  it("requires the complete anchor for each supported scope kind", () => {
    expect(hasAnchor({ ...baseForm, accountId: "123456789012" })).toBe(true);
    expect(
      hasAnchor({
        ...baseForm,
        accountId: "123456789012",
        scopeKind: "region",
      }),
    ).toBe(false);
    expect(
      hasAnchor({
        ...baseForm,
        accountId: "123456789012",
        region: "us-east-1",
        scopeKind: "region",
      }),
    ).toBe(true);
    expect(hasAnchor({ ...baseForm, accountId: "123456789012", scopeKind: "service" })).toBe(false);
    expect(
      hasAnchor({
        ...baseForm,
        scopeId: "aws:123456789012:us-east-1:lambda",
        scopeKind: "service",
      }),
    ).toBe(true);
  });
});
