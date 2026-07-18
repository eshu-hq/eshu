import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";

import { ReplatformingPage } from "./ReplatformingPage";
import type { EshuApiClient } from "../api/client";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";

describe("ReplatformingPage", () => {
  it("loads a deep-linked read-only plan with rollups, import candidates, and ownership packets", async () => {
    const calls: { readonly body: unknown; readonly path: string }[] = [];
    const client = {
      get: async () => selectorEnvelope(),
      post: async (path: string, body: unknown) => {
        calls.push({ body, path });
        if (path.endsWith("/rollups")) {
          return {
            data: rollupsPayload(),
            error: null,
            truth: truthEnvelope("replatforming.rollups.readiness"),
          };
        }
        if (path.endsWith("/plans")) {
          return {
            data: planPayload(),
            error: null,
            truth: truthEnvelope("replatforming.plan.readiness"),
          };
        }
        return {
          data: ownershipPayload(),
          error: null,
          truth: truthEnvelope("replatforming.ownership.candidates"),
        };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter
        initialEntries={[
          "/replatforming?account_id=123456789012&region=us-east-1&scope_kind=account",
        ]}
      >
        <ReplatformingPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
      </MemoryRouter>,
    );

    expect(await screen.findByRole("heading", { name: "Replatforming plans" })).toBeInTheDocument();
    await waitFor(() => {
      expect(calls.map((call) => call.path)).toEqual([
        "/api/v0/replatforming/rollups",
        "/api/v0/replatforming/plans",
        "/api/v0/replatforming/ownership-packets",
      ]);
    });
    expect(screen.getByText("read only")).toBeInTheDocument();
    expect(screen.getByText("does not run Terraform or any migration")).toBeInTheDocument();
    expect(screen.getByText("replatforming.plan.readiness")).toBeInTheDocument();
    expect(
      screen.getByText("4 active AWS replatforming findings matched account scope."),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Showing up to 100 rows from offset 25. Next offset 125."),
    ).toBeInTheDocument();
    expect(screen.getAllByText("payments-api").length).toBeGreaterThan(0);
    expect(screen.getByText("ready")).toBeInTheDocument();
    expect(screen.getByText("security review required")).toBeInTheDocument();
    expect(screen.getByText("terraform state address")).toBeInTheDocument();
  });

  it("submits filters into a deep-linkable bounded URL", async () => {
    const calls: { readonly body: Record<string, unknown>; readonly path: string }[] = [];
    const client = {
      get: async () => selectorEnvelope(),
      post: async (path: string, body: Record<string, unknown>) => {
        calls.push({ body, path });
        return {
          data: path.endsWith("/rollups")
            ? rollupsPayload()
            : path.endsWith("/plans")
              ? planPayload()
              : ownershipPayload(),
          error: null,
          truth: truthEnvelope("replatforming.plan.readiness"),
        };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/replatforming"]}>
        <Routes>
          <Route
            element={
              <>
                <ReplatformingPage
                  client={client}
                  model={modelFromSnapshot(emptySnapshot("live"))}
                />
                <LocationProbe />
              </>
            }
            path="/replatforming"
          />
        </Routes>
      </MemoryRouter>,
    );

    await screen.findByRole("heading", { name: "Replatforming plans" });
    expect(
      await screen.findByText(
        "Choose an account, region, or source scope to review a bounded plan.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText("No replatforming data loaded.")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Account"), { target: { value: "123456789012" } });
    fireEvent.change(screen.getByLabelText("Region"), { target: { value: "us-east-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Review plan" }));

    expect(await screen.findByTestId("replatforming-location")).toHaveTextContent(
      "/replatforming?scope_kind=account&account_id=123456789012&region=us-east-1",
    );
    await waitFor(() => expect(calls).toHaveLength(3));
    expect(calls.find((call) => call.path.endsWith("/plans"))?.body).toMatchObject({
      account_id: "123456789012",
      limit: 100,
      offset: 0,
      region: "us-east-1",
      scope_kind: "account",
    });
  });

  it("loads selector inventory before showing a truthful unqueried state", async () => {
    let resolveSelectors: ((value: ReturnType<typeof selectorEnvelope>) => void) | undefined;
    const selectors = new Promise<ReturnType<typeof selectorEnvelope>>((resolve) => {
      resolveSelectors = resolve;
    });
    const post = vi.fn();
    const client = {
      get: vi.fn(async () => selectors),
      post,
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/replatforming"]}>
        <ReplatformingPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
      </MemoryRouter>,
    );

    expect(screen.getByText("Loading selector inventory...")).toBeInTheDocument();
    expect(screen.queryByText("No replatforming data loaded.")).not.toBeInTheDocument();
    resolveSelectors?.(selectorEnvelope());

    expect(
      await screen.findByText(
        "Choose an account, region, or source scope to review a bounded plan.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Account" })).toBeInTheDocument();
    expect(screen.getByRole("listbox", { name: "Finding kinds" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Page size" })).toHaveValue("100");
    expect(screen.getByRole("button", { name: "Review plan" })).toBeDisabled();
    expect(document.querySelector("#replatforming-accounts option")).toHaveAttribute(
      "label",
      expect.stringContaining("active scopes"),
    );
    expect(post).not.toHaveBeenCalled();
  });

  it("adapts controls to scope kind and reports a bounded selector slice", async () => {
    const envelope = selectorEnvelope();
    const client = {
      get: vi.fn(async () => ({
        ...envelope,
        data: { ...envelope.data, truncated: true },
      })),
      post: vi.fn(),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/replatforming"]}>
        <ReplatformingPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
      </MemoryRouter>,
    );

    expect(await screen.findByText(/first 200 authorized scopes/i)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Scope kind"), { target: { value: "service" } });

    expect(screen.getByRole("combobox", { name: "Source scope" })).toBeInTheDocument();
    expect(screen.queryByRole("combobox", { name: "Account" })).not.toBeInTheDocument();
    expect(screen.queryByRole("combobox", { name: "Region" })).not.toBeInTheDocument();
  });

  it("moves through bounded review pages without exposing raw offsets", async () => {
    const calls: Record<string, unknown>[] = [];
    const client = {
      get: async () => selectorEnvelope(),
      post: async (path: string, body: Record<string, unknown>) => {
        calls.push(body);
        return {
          data: path.endsWith("/rollups")
            ? rollupsPayload()
            : path.endsWith("/plans")
              ? planPayload()
              : ownershipPayload(),
          error: null,
          truth: truthEnvelope("replatforming.plan.readiness"),
        };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter
        initialEntries={["/replatforming?scope_kind=account&account_id=123456789012&offset=25"]}
      >
        <Routes>
          <Route
            element={
              <>
                <ReplatformingPage
                  client={client}
                  model={modelFromSnapshot(emptySnapshot("live"))}
                />
                <LocationProbe />
              </>
            }
            path="/replatforming"
          />
        </Routes>
      </MemoryRouter>,
    );

    await screen.findByText("Showing up to 100 rows from offset 25. Next offset 125.");
    expect(screen.queryByLabelText("Offset")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));

    expect(await screen.findByTestId("replatforming-location")).toHaveTextContent(
      "/replatforming?scope_kind=account&account_id=123456789012&offset=125",
    );
    await waitFor(() => expect(calls.some((body) => body.offset === 125)).toBe(true));
    fireEvent.click(screen.getByRole("button", { name: "Previous page" }));
    expect(await screen.findByTestId("replatforming-location")).toHaveTextContent(
      "/replatforming?scope_kind=account&account_id=123456789012&offset=25",
    );
  });

  it("ignores a stale plan response after a newer selection completes", async () => {
    let resolveFirst: (() => void) | undefined;
    const firstRelease = new Promise<void>((resolve) => {
      resolveFirst = resolve;
    });
    const post = vi.fn(async (path: string, body: Record<string, unknown>) => {
      if (body.account_id === "123456789012") await firstRelease;
      const suffix = body.account_id === "210987654321" ? "new selection" : "stale selection";
      const data = path.endsWith("/rollups")
        ? { ...rollupsPayload(), story: suffix }
        : path.endsWith("/plans")
          ? { ...planPayload(), story: suffix }
          : { ...ownershipPayload(), story: suffix };
      return { data, error: null, truth: truthEnvelope("replatforming.plan.readiness") };
    });
    const client = {
      get: async () => selectorEnvelopeTwoAccounts(),
      post,
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/replatforming"]}>
        <ReplatformingPage client={client} model={modelFromSnapshot(emptySnapshot("live"))} />
      </MemoryRouter>,
    );

    await screen.findByText("Choose an account, region, or source scope to review a bounded plan.");
    fireEvent.change(screen.getByLabelText("Account"), { target: { value: "123456789012" } });
    fireEvent.click(screen.getByRole("button", { name: "Review plan" }));
    fireEvent.change(screen.getByLabelText("Account"), { target: { value: "210987654321" } });
    fireEvent.click(screen.getByRole("button", { name: "Review plan" }));

    expect(await screen.findAllByText("new selection")).not.toHaveLength(0);
    resolveFirst?.();
    await waitFor(() => expect(screen.queryByText("stale selection")).not.toBeInTheDocument());
  });
});

function LocationProbe(): React.JSX.Element {
  const location = useLocation();
  return (
    <output data-testid="replatforming-location">{location.pathname + location.search}</output>
  );
}

function truthEnvelope(capability: string) {
  return {
    basis: "semantic_facts",
    capability,
    freshness: { state: "fresh" },
    level: "derived",
    profile: "local_authoritative",
  };
}

function selectorEnvelope() {
  return {
    data: {
      count: 2,
      empty_scope_count: 1,
      finding_kinds: [
        "ambiguous_cloud_resource",
        "orphaned_cloud_resource",
        "unmanaged_cloud_resource",
        "unknown_cloud_resource",
      ],
      limit: 200,
      page_sizes: [25, 50, 100, 200],
      readiness: {
        detail: "2 active AWS collector scope(s) are available.",
        next_action: "Choose an account, region, or source scope to review a bounded plan.",
        state: "ready",
      },
      scopes: [
        {
          account_id: "123456789012",
          finding_count: 3,
          label: "lambda in us-east-1 (account ...9012)",
          region: "us-east-1",
          scope_id: "aws:123456789012:us-east-1:lambda",
          service: "lambda",
        },
        {
          account_id: "123456789012",
          finding_count: 0,
          label: "s3 in us-west-2 (account ...9012)",
          region: "us-west-2",
          scope_id: "aws:123456789012:us-west-2:s3",
          service: "s3",
        },
      ],
      supported_scope_kinds: ["account", "region", "service"],
      truncated: false,
    },
    error: null,
    truth: truthEnvelope("replatforming.selector_inventory"),
  };
}

function selectorEnvelopeTwoAccounts() {
  const envelope = selectorEnvelope();
  return {
    ...envelope,
    data: {
      ...envelope.data,
      count: 3,
      scopes: [
        ...envelope.data.scopes,
        {
          account_id: "210987654321",
          finding_count: 2,
          label: "ec2 in us-east-2 (account ...4321)",
          region: "us-east-2",
          scope_id: "aws:210987654321:us-east-2:ec2",
          service: "ec2",
        },
      ],
    },
  };
}

function rollupsPayload(): Record<string, unknown> {
  return {
    dimensions: {
      account: [
        {
          key: "123456789012",
          readiness: { import_ready: 1, needs_review: 2, refused: 1 },
          source_state_counts: { derived: 3, rejected: 1 },
          total: 4,
        },
      ],
      environment: [
        {
          key: "prod",
          readiness: { import_ready: 1, needs_review: 1, refused: 1 },
          source_state_counts: { derived: 2, rejected: 1 },
          total: 3,
        },
      ],
      service: [
        {
          key: "payments-api",
          readiness: { import_ready: 1, needs_review: 0, refused: 1 },
          source_state_counts: { derived: 1, rejected: 1 },
          total: 2,
        },
      ],
    },
    readiness_totals: { import_ready: 1, needs_review: 2, refused: 1 },
    rollup_findings_count: 4,
    source_state_totals: { derived: 3, rejected: 1 },
    story: "4 active AWS replatforming findings matched account scope.",
    total_findings_count: 4,
    truncated: false,
  };
}

function planPayload(): Record<string, unknown> {
  return {
    blast_radius_summaries: [{ group_id: "low", item_count: 1, severity: "low" }],
    items_count: 2,
    limit: 100,
    next_offset: 125,
    offset: 25,
    plan: {
      items: [
        {
          blast_radius_group: "low",
          import_candidate: {
            import_block:
              'import { to = aws_lambda_function.payments id = "arn:aws:lambda:us-east-1:123456789012:function:payments-api" }',
            status: "ready",
          },
          item_id: "fact:ready-lambda",
          owner_candidates: [{ confidence: "derived", kind: "service", value: "payments-api" }],
          provider: "aws",
          resource_type: "lambda",
          safety_gate: { outcome: "allowed", review_required: false },
          source_state: "derived",
          stable_id: "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
          wave_id: "wave-1-early-safe",
        },
        {
          import_candidate: { refusal_reasons: ["security_review_required"], status: "refused" },
          item_id: "fact:blocked",
          provider: "aws",
          resource_type: "iam",
          safety_gate: { outcome: "security_review_required", review_required: true },
          source_state: "rejected",
          stable_id: "arn:aws:iam::123456789012:role/app",
          wave_id: "wave-3-blocked",
        },
      ],
      non_goals: [
        "does not run Terraform or any migration",
        "does not import resources or mutate cloud state",
        "does not write user repositories",
      ],
      waves: [
        {
          id: "wave-1-early-safe",
          item_ids: ["fact:ready-lambda"],
          order: 1,
          rationale: "ready candidates first",
        },
      ],
    },
    ready_import_count: 1,
    refused_import_count: 1,
    story: "2 migration packet items composed for account scope.",
    total_findings_count: 2,
    truncated: true,
    wave_summaries: [{ item_count: 1, order: 1, wave_id: "wave-1-early-safe" }],
  };
}

function ownershipPayload(): Record<string, unknown> {
  return {
    ambiguous_count: 1,
    ownership_packets: [
      {
        freshness: { state: "fresh" },
        item_id: "fact:ready-lambda",
        missing_evidence: ["terraform_state_address"],
        owner_candidates: [{ confidence: "derived", kind: "service", value: "payments-api" }],
        provider: "aws",
        safety_gate: { outcome: "allowed", review_required: false },
        source_state: "derived",
        stable_id: "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
      },
    ],
    packets_count: 1,
    rejected_count: 0,
    story: "1 ownership packet composed.",
    truncated: false,
    unattributed_count: 0,
  };
}
