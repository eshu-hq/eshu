import { factoryOnlyUsedFromSpec } from "./test-only-reference";

export function makeAccountFixture(): { id: string } {
  return factoryOnlyUsedFromSpec("fixture-account");
}
