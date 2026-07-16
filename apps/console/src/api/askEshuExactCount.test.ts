import { describe, expect, it } from "vitest";

import { normalizeAnswer } from "./askEshuNormalize";

describe("Ask Eshu exact indexed-repository result", () => {
  it("preserves the canonical aggregate result reference and total", () => {
    const answer = normalizeAnswer({
      answer_prose:
        "896 indexed repositories visible in your authorized scope. Evidence: list_indexed_repositories.total.",
      truth_class: "deterministic",
      result_ref: "eshu://api-result/repositories",
      result: { total: 896 },
      query_trace: [
        {
          tool: "list_indexed_repositories",
          supported: true,
          truth_class: "deterministic",
        },
      ],
    });

    expect(answer.result_ref).toBe("eshu://api-result/repositories");
    expect(answer.result).toEqual({ total: 896 });
    expect(answer.query_trace).toEqual([
      {
        tool: "list_indexed_repositories",
        supported: true,
        truth_class: "deterministic",
      },
    ]);
  });
});
