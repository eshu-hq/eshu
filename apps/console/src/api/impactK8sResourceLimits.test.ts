import { normalizeK8sResourceLimits } from "./impactK8sResourceLimits";

describe("normalizeK8sResourceLimits", () => {
  it.each([
    [
      "the aggregate lower-bound flag ignores a saturated deployment-source scan",
      {
        deployment_source_observed_count_is_lower_bound: true,
        observed_count_is_lower_bound: false,
        truncated: true,
      },
    ],
    [
      "the aggregate lower-bound flag is set without a saturated constituent",
      {
        observed_count_is_lower_bound: true,
        deployment_source_observed_count_is_lower_bound: false,
      },
    ],
    ["the aggregate count exceeds the constituent observations", { observed_count: 3 }],
  ])("fails closed when %s", (_reason, override) => {
    expect(normalizeK8sResourceLimits({ ...validLimits(), ...override }, 1)).toBeNull();
  });

  it.each([
    "content_observed_count",
    "content_observed_count_is_lower_bound",
    "deployment_source_observed_count",
    "deployment_source_observed_count_is_lower_bound",
    "deployment_source_query_sentinel_limit",
  ])("fails closed when %s is missing", (field) => {
    const limits = validLimits();
    delete limits[field];

    expect(normalizeK8sResourceLimits(limits, 1)).toBeNull();
  });

  it("accepts a valid deployment-source sentinel saturation as truncated", () => {
    expect(
      normalizeK8sResourceLimits(
        {
          ...validLimits(),
          deployment_source_observed_count_is_lower_bound: true,
          observed_count_is_lower_bound: true,
          truncated: true,
        },
        1,
      ),
    ).toMatchObject({
      deploymentSourceObservedCountIsLowerBound: true,
      observedCountIsLowerBound: true,
      truncated: true,
    });
  });
});

function validLimits(): Record<string, unknown> {
  return {
    content_observed_count: 0,
    content_observed_count_is_lower_bound: false,
    deployment_source_observed_count: 1,
    deployment_source_observed_count_is_lower_bound: false,
    deployment_source_query_sentinel_limit: 201,
    limit: 50,
    observed_count: 1,
    observed_count_is_lower_bound: false,
    ordering: ["repo_id", "relative_path", "entity_id"],
    query_sentinel_limit: 51,
    returned_count: 1,
    truncated: false,
  };
}
