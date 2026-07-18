export function publicContext(): Record<string, unknown> {
  return {
    name: "checkout",
    entrypoints: [{ type: "hostname", target: "checkout.example.test", visibility: "public" }],
    network_paths: [
      {
        path_type: "hostname_to_runtime",
        from_type: "hostname",
        from: "checkout.example.test",
        to_type: "runtime_platform",
        to: "checkout-eks",
        platform_kind: "eks",
        environment: "production",
        visibility: "public",
        reason: "ingress host maps to the eks runtime",
      },
    ],
    ingress_posture: {
      waf_coverage: "protected",
      tls_termination: "terminated",
      edge_count: 1,
      waf_protected: 1,
      tls_terminated: 1,
      reason: "observed across 1 internet-facing edge resource",
    },
  };
}

export function internalContext(): Record<string, unknown> {
  return {
    name: "internal-api",
    entrypoints: [{ type: "docs_route", target: "/internal/health", visibility: "internal" }],
    network_paths: [
      {
        path_type: "docs_route_to_runtime",
        from_type: "docs_route",
        from: "/internal/health",
        to_type: "runtime_platform",
        to: "internal-eks",
        platform_kind: "eks",
        visibility: "internal",
      },
    ],
  };
}

export function serviceOptions(): readonly {
  readonly environments: readonly string[];
  readonly freshness: "fresh";
  readonly id: string;
  readonly kind: string;
  readonly name: string;
  readonly repo: string;
  readonly truth: "exact";
}[] {
  return [
    serviceOption("workload:checkout", "Checkout API", "checkout-service"),
    serviceOption("workload:internal-api", "Internal API", "internal-api"),
    serviceOption("workload:ghost", "Ghost", "ghost-service"),
    serviceOption("workload:payments", "Payments API", "payments-service"),
  ];
}

export function contextEnvelope(
  options: { readonly hostname?: string; readonly name?: string } = {},
): {
  readonly data: Record<string, unknown>;
  readonly error: null;
  readonly truth: null;
} {
  const hostname = options.hostname ?? "checkout.example.test";
  const name = options.name ?? "checkout";
  return {
    data: {
      ...publicContext(),
      entrypoints: [{ type: "hostname", target: hostname, visibility: "public" }],
      name,
      network_paths: [
        {
          from: hostname,
          from_type: "hostname",
          platform_kind: "eks",
          to: `${name}-eks`,
          to_type: "runtime_platform",
          visibility: "public",
        },
      ],
    },
    error: null,
    truth: null,
  };
}

function serviceOption(id: string, name: string, repo: string) {
  return {
    environments: ["production"],
    freshness: "fresh" as const,
    id,
    kind: "service",
    name,
    repo,
    truth: "exact" as const,
  };
}
