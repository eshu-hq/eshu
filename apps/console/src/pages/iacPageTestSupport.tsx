import { render } from "@testing-library/react";
import { MemoryRouter, useLocation, useNavigate } from "react-router-dom";

export function iacEnvelope(
  resources: readonly Record<string, unknown>[],
  opts: {
    readonly truncated?: boolean;
    readonly afterName?: string;
    readonly afterId?: string;
  } = {},
) {
  return {
    data: {
      count: resources.length,
      kind: "resource",
      limit: 25,
      resources,
      truncated: opts.truncated === true,
      next_cursor:
        opts.truncated === true
          ? { after_name: opts.afterName ?? "next", after_id: opts.afterId ?? "id-next" }
          : undefined,
    },
    error: null,
    truth: {
      capability: "iac_inventory.resources.list",
      freshness: { state: "fresh" },
      level: "derived",
      profile: "production",
    },
  };
}

export function authoritativeIacEnvelope(resources: readonly Record<string, unknown>[]) {
  const base = iacEnvelope(resources);
  return {
    ...base,
    data: {
      ...base.data,
      summary: {
        total: 24610,
        by_kind: { resource: 17117, module: 612, "data-source": 6881 },
        types: [{ kind: "resource", value: "aws_s3_bucket", count: 500 }],
        providers: [{ kind: "resource", value: "aws", count: 1000 }],
        modules: [{ value: "audit", count: 25 }],
        repositories: [{ value: "repository:r1", count: 100 }],
        facet_limit: 200,
        truncated: { types: true },
      },
    },
  };
}

export function iacRow(id: string, name: string) {
  return {
    id,
    kind: "resource",
    name,
    resource_name: name.split(".").at(-1),
    type: "aws_s3_bucket",
    provider: "aws",
    resource_service: "s3",
    resource_category: "storage",
    module: "audit",
    repo_id: "repository:r1",
    relative_path: "logging.tf",
    line_number: 12,
  };
}

export function renderIacPage(
  page: React.ReactElement,
  initialEntries: string[] = ["/iac"],
  initialIndex?: number,
) {
  return render(
    <MemoryRouter initialEntries={initialEntries} initialIndex={initialIndex}>
      {page}
    </MemoryRouter>,
  );
}

export function LocationProbe(): React.JSX.Element {
  const location = useLocation();
  return <span data-testid="iac-location">{location.pathname + location.search}</span>;
}

export function BackButton(): React.JSX.Element {
  const navigate = useNavigate();
  return <button onClick={() => navigate(-1)}>Browser back</button>;
}

export function NavigateArchiveButton(): React.JSX.Element {
  const navigate = useNavigate();
  return <button onClick={() => navigate("/iac?q=archive")}>Navigate to archive</button>;
}
