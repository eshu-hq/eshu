import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";

import { IacPage } from "./IacPage";
import {
  authoritativeIacEnvelope as authoritativeEnvelope,
  BackButton,
  iacEnvelope as envelope,
  iacRow as row,
  LocationProbe,
  NavigateArchiveButton,
  renderIacPage,
} from "./iacPageTestSupport";
import type { EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

describe("IacPage", () => {
  it("renders the IaC inventory from the model", () => {
    renderIacPage(<IacPage model={demoModel} />);

    expect(screen.getByRole("heading", { name: "IaC Inventory" })).toBeInTheDocument();
    expect(screen.getByText("GET /api/v0/iac/resources")).toBeInTheDocument();
    expect(screen.getByLabelText("IaC evidence workbench")).toBeInTheDocument();
    expect(screen.getByText("Resources (current)")).toBeInTheDocument();
    // Resource rows render with their Terraform type.
    expect(screen.getByText('module."checkout".aws_iam_role.this')).toBeInTheDocument();
    expect(screen.getByText("aws_s3_bucket.assets")).toBeInTheDocument();
    // The filter controls are present and labeled for accessibility.
    expect(screen.getByLabelText("Search IaC resources")).toBeInTheDocument();
    expect(screen.getByLabelText("Filter by type")).toBeInTheDocument();
    expect(screen.getByLabelText("Filter by module")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Search IaC resources"), { target: { value: "s3" } });
    expect(screen.queryByText('module."checkout".aws_iam_role.this')).not.toBeInTheDocument();
    expect(screen.getByText("aws_s3_bucket.assets")).toBeInTheDocument();
  });

  it("renders the empty state when no resources are indexed", () => {
    const empty: ConsoleModel = { ...demoModel, iacResources: [] };
    renderIacPage(<IacPage model={empty} />);
    expect(
      screen.getByText("No Terraform/IaC resources have been indexed yet."),
    ).toBeInTheDocument();
  });

  it("renders the unavailable state when the section is unavailable", () => {
    const unavailable: ConsoleModel = {
      ...demoModel,
      iacResources: [],
      provenance: { ...demoModel.provenance, iacResources: "unavailable" },
    };
    renderIacPage(<IacPage model={unavailable} />);
    expect(
      screen.getByText(
        "IaC inventory is not available from this API (it requires the authoritative graph profile).",
      ),
    ).toBeInTheDocument();
  });

  it("shows demo fixture rows and does not call the API when sourceLabel is demo fixtures", async () => {
    const get = vi.fn();
    const client = { get } as unknown as EshuApiClient;

    renderIacPage(<IacPage client={client} sourceLabel="demo fixtures" model={demoModel} />);

    expect(await screen.findByText('module."checkout".aws_iam_role.this')).toBeInTheDocument();
    expect(screen.getByText("aws_s3_bucket.assets")).toBeInTheDocument();
    expect(screen.getByText("bounded page from the graph")).toBeInTheDocument();
    expect(get).not.toHaveBeenCalled();
  });

  it("does not render bounded bootstrap rows while the live inventory read is pending", async () => {
    const get = vi.fn(() => new Promise(() => undefined));
    const client = { get } as unknown as EshuApiClient;

    renderIacPage(<IacPage client={client} sourceLabel="live" model={demoModel} />);

    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    expect(screen.queryByText('module."checkout".aws_iam_role.this')).not.toBeInTheDocument();
    expect(screen.queryByText("aws_s3_bucket.assets")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Loading…" })).toBeDisabled();
  });

  it("does not inherit unavailable bootstrap provenance while a live read is pending", async () => {
    const get = vi.fn(() => new Promise(() => undefined));
    const client = { get } as unknown as EshuApiClient;
    const model: ConsoleModel = {
      ...demoModel,
      iacResources: [],
      provenance: { ...demoModel.provenance, iacResources: "unavailable" },
    };

    renderIacPage(<IacPage client={client} sourceLabel="live" model={model} />);

    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    expect(screen.getByText("Loading current IaC inventory…")).toBeInTheDocument();
    expect(
      screen.queryByText(
        "IaC inventory is not available from this API (it requires the authoritative graph profile).",
      ),
    ).not.toBeInTheDocument();
  });

  it("does not fall back to bootstrap truth when a live response has no truth metadata", async () => {
    const response = { ...envelope([row("r1", "aws_s3_bucket.logs")]), truth: null };
    const get = vi.fn().mockResolvedValue(response);
    const client = { get } as unknown as EshuApiClient;
    const model: ConsoleModel = {
      ...demoModel,
      iacResources: [],
      truth: {
        ...demoModel.truth,
        iacResources: {
          capability: "iac_inventory.resources.list",
          freshness: { state: "fresh" },
          level: "exact",
          profile: "production",
        },
      },
    };

    renderIacPage(<IacPage client={client} sourceLabel="live" model={model} />);

    await waitFor(() => expect(screen.getByText("aws_s3_bucket.logs")).toBeInTheDocument());
    expect(screen.queryByTitle("Truth: exact")).not.toBeInTheDocument();
    expect(screen.queryByTitle("Freshness: fresh")).not.toBeInTheDocument();
  });

  it("clears stale live rows and shows fixture model when switching live->demo", async () => {
    const get = vi.fn().mockResolvedValue(envelope([row("live-1", "aws_s3_bucket.private-live")]));
    const client = { get } as unknown as EshuApiClient;
    const { rerender } = renderIacPage(
      <IacPage client={client} sourceLabel="live" model={{ ...demoModel, iacResources: [] }} />,
    );

    // Live rows appear after fetch resolves.
    await waitFor(() => expect(screen.getByText("aws_s3_bucket.private-live")).toBeInTheDocument());

    // Simulate switching to demo mode: sourceLabel flips to "demo fixtures".
    rerender(
      <MemoryRouter>
        <IacPage client={client} sourceLabel="demo fixtures" model={demoModel} />
      </MemoryRouter>,
    );

    // Private live row must be gone; demo fixture rows must appear instead.
    expect(screen.queryByText("aws_s3_bucket.private-live")).not.toBeInTheDocument();
    expect(screen.getByText('module."checkout".aws_iam_role.this')).toBeInTheDocument();
    expect(screen.getByText("aws_s3_bucket.assets")).toBeInTheDocument();
    // No further API calls should have been made after the switch.
    const callCountAtSwitch = get.mock.calls.length;
    expect(callCountAtSwitch).toBeGreaterThanOrEqual(1);
    // No new calls after rerender.
    expect(get.mock.calls.length).toBe(callCountAtSwitch);
  });

  it("loads and pages IaC resources directly from the live API", async () => {
    const get = vi
      .fn()
      .mockResolvedValueOnce(
        envelope([row("r1", "aws_s3_bucket.logs")], {
          truncated: true,
          afterName: "aws_s3_bucket.logs",
          afterId: "r1",
        }),
      )
      .mockResolvedValueOnce(envelope([row("r2", "aws_s3_bucket.archive")]));
    const client = { get } as unknown as EshuApiClient;

    renderIacPage(<IacPage client={client} model={{ ...demoModel, iacResources: [] }} />);

    await waitFor(() => expect(screen.getByText("aws_s3_bucket.logs")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /Next/ }));

    await waitFor(() => expect(screen.getByText("aws_s3_bucket.archive")).toBeInTheDocument());
    const lastCall = get.mock.calls[get.mock.calls.length - 1][0] as string;
    expect(lastCall).toContain("after_name=aws_s3_bucket.logs");
    expect(lastCall).toContain("after_id=r1");
    expect(lastCall).not.toContain("offset");
  });

  it("clears the prior URL-owned view while a replacement query is pending", async () => {
    const get = vi
      .fn()
      .mockResolvedValueOnce(envelope([row("r1", "aws_s3_bucket.logs")]))
      .mockImplementationOnce(() => new Promise(() => undefined));
    const client = { get } as unknown as EshuApiClient;

    renderIacPage(
      <>
        <IacPage client={client} model={{ ...demoModel, iacResources: [] }} />
        <NavigateArchiveButton />
      </>,
    );

    await waitFor(() => expect(screen.getByText("aws_s3_bucket.logs")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Navigate to archive" }));

    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    expect(screen.queryByText("aws_s3_bucket.logs")).not.toBeInTheDocument();
    expect(screen.getByText("Loading current IaC inventory…")).toBeInTheDocument();
  });

  it("renders every row in the bounded live server page", async () => {
    const resources = Array.from({ length: 30 }, (_, index) =>
      row(`r${index + 1}`, `aws_s3_bucket.page_${String(index + 1).padStart(2, "0")}`),
    );
    const get = vi.fn().mockResolvedValue(envelope(resources));
    const client = { get } as unknown as EshuApiClient;

    renderIacPage(<IacPage client={client} model={{ ...demoModel, iacResources: [] }} />);

    await waitFor(() => expect(screen.getByText("aws_s3_bucket.page_30")).toBeInTheDocument());
    expect(screen.getByText("aws_s3_bucket.page_01")).toBeInTheDocument();
    expect(screen.getAllByRole("row")).toHaveLength(31);
  });

  it("uses authoritative totals, server search, repository facets, and exact source links", async () => {
    const get = vi.fn().mockResolvedValue(authoritativeEnvelope([row("r1", "aws_s3_bucket.logs")]));
    const client = { get } as unknown as EshuApiClient;

    renderIacPage(<IacPage client={client} model={{ ...demoModel, iacResources: [] }} />);

    await waitFor(() => expect(screen.getByText("17,117")).toBeInTheDocument());
    expect(screen.getByText("24,610 current IaC objects")).toBeInTheDocument();
    expect(screen.getByText("1+")).toBeInTheDocument();
    expect(screen.getByLabelText("Filter by repository")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "logging.tf:12" })).toHaveAttribute(
      "href",
      "/repositories/repository%3Ar1/source?path=logging.tf&lineStart=12",
    );

    fireEvent.change(screen.getByLabelText("Search IaC resources"), { target: { value: "logs" } });
    fireEvent.change(screen.getByLabelText("Filter by repository"), {
      target: { value: "repository:r1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    const path = get.mock.calls[1][0] as string;
    expect(path).toContain("q=logs");
    expect(path).toContain("repository=repository%3Ar1");
    expect(path).toContain("include_facets=true");
  });

  it("renders rows accepted by the authoritative server search", async () => {
    const get = vi.fn().mockResolvedValue(authoritativeEnvelope([row("r1", "aws_s3_bucket.logs")]));
    const client = { get } as unknown as EshuApiClient;

    renderIacPage(<IacPage client={client} model={{ ...demoModel, iacResources: [] }} />, [
      "/iac?q=TerraformResource&kind=resource",
    ]);

    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    expect(get.mock.calls[0][0]).toContain("q=TerraformResource");
    expect(screen.getByText("aws_s3_bucket.logs")).toBeInTheDocument();
  });

  it("aborts a superseded inventory request", async () => {
    const get = vi.fn(
      (_path: string, options?: { readonly signal?: AbortSignal }) =>
        new Promise((resolve, reject) => {
          options?.signal?.addEventListener("abort", () => reject(new Error("request aborted")));
          if (get.mock.calls.length > 1) resolve(authoritativeEnvelope([]));
        }),
    );
    const client = { get } as unknown as EshuApiClient;

    renderIacPage(
      <>
        <IacPage client={client} model={{ ...demoModel, iacResources: [] }} />
        <NavigateArchiveButton />
      </>,
    );
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    const firstSignal = get.mock.calls[0][1]?.signal as AbortSignal | undefined;

    fireEvent.click(screen.getByRole("button", { name: "Navigate to archive" }));

    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    expect(firstSignal?.aborted).toBe(true);
  });

  it("aborts the active pagination request when the page unmounts", async () => {
    const get = vi
      .fn<(_path: string, options?: { readonly signal?: AbortSignal }) => Promise<unknown>>()
      .mockResolvedValueOnce(
        envelope([row("r1", "aws_s3_bucket.logs")], {
          truncated: true,
          afterName: "aws_s3_bucket.logs",
          afterId: "r1",
        }),
      )
      .mockImplementationOnce(
        (_path: string, options?: { readonly signal?: AbortSignal }) =>
          new Promise((_resolve, reject) => {
            options?.signal?.addEventListener("abort", () => reject(new Error("request aborted")));
          }),
      );
    const client = { get } as unknown as EshuApiClient;
    const rendered = renderIacPage(
      <IacPage client={client} model={{ ...demoModel, iacResources: [] }} />,
    );

    await waitFor(() => expect(screen.getByText("aws_s3_bucket.logs")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    const pageSignal = get.mock.calls[1][1]?.signal as AbortSignal | undefined;

    rendered.unmount();

    expect(pageSignal?.aborted).toBe(true);
  });

  it("leaves loading state when a pending live request switches to demo", async () => {
    const get = vi.fn(
      (_path: string, options?: { readonly signal?: AbortSignal }) =>
        new Promise((_resolve, reject) => {
          options?.signal?.addEventListener("abort", () => reject(new Error("request aborted")));
        }),
    );
    const client = { get } as unknown as EshuApiClient;
    const rendered = renderIacPage(
      <IacPage client={client} sourceLabel="live" model={{ ...demoModel, iacResources: [] }} />,
    );

    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    const liveSignal = get.mock.calls[0][1]?.signal as AbortSignal | undefined;
    expect(screen.getByRole("button", { name: "Loading…" })).toBeDisabled();

    rendered.rerender(
      <MemoryRouter>
        <IacPage client={client} sourceLabel="demo fixtures" model={demoModel} />
      </MemoryRouter>,
    );

    expect(liveSignal?.aborted).toBe(true);
    expect(screen.getByRole("button", { name: "Apply" })).toBeEnabled();
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.queryByText(/Failed to load IaC resources:/)).not.toBeInTheDocument();
  });

  it("does not surface an aborted pagination error after switching to demo", async () => {
    const get = vi
      .fn<(_path: string, options?: { readonly signal?: AbortSignal }) => Promise<unknown>>()
      .mockResolvedValueOnce(
        envelope([row("r1", "aws_s3_bucket.logs")], {
          truncated: true,
          afterName: "aws_s3_bucket.logs",
          afterId: "r1",
        }),
      )
      .mockImplementationOnce(
        (_path: string, options?: { readonly signal?: AbortSignal }) =>
          new Promise((_resolve, reject) => {
            options?.signal?.addEventListener("abort", () => reject(new Error("request aborted")));
          }),
      );
    const client = { get } as unknown as EshuApiClient;
    const rendered = renderIacPage(
      <IacPage client={client} sourceLabel="live" model={{ ...demoModel, iacResources: [] }} />,
    );

    await waitFor(() => expect(screen.getByText("aws_s3_bucket.logs")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));

    rendered.rerender(
      <MemoryRouter>
        <IacPage client={client} sourceLabel="demo fixtures" model={demoModel} />
      </MemoryRouter>,
    );
    await act(async () => {
      await Promise.resolve();
    });

    expect(screen.queryByText(/Failed to load IaC resources:/)).not.toBeInTheDocument();
    expect(screen.getByText("aws_s3_bucket.assets")).toBeInTheDocument();
  });

  it("aborts pagination when the page switches to model-only mode", async () => {
    const get = vi
      .fn<(_path: string, options?: { readonly signal?: AbortSignal }) => Promise<unknown>>()
      .mockResolvedValueOnce(
        envelope([row("r1", "aws_s3_bucket.logs")], {
          truncated: true,
          afterName: "aws_s3_bucket.logs",
          afterId: "r1",
        }),
      )
      .mockImplementationOnce(
        (_path: string, options?: { readonly signal?: AbortSignal }) =>
          new Promise((_resolve, reject) => {
            options?.signal?.addEventListener("abort", () => reject(new Error("request aborted")));
          }),
      );
    const client = { get } as unknown as EshuApiClient;
    const rendered = renderIacPage(
      <IacPage client={client} sourceLabel="live" model={{ ...demoModel, iacResources: [] }} />,
    );

    await waitFor(() => expect(screen.getByText("aws_s3_bucket.logs")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    const pageSignal = get.mock.calls[1][1]?.signal as AbortSignal | undefined;

    rendered.rerender(
      <MemoryRouter>
        <IacPage model={demoModel} />
      </MemoryRouter>,
    );

    expect(pageSignal?.aborted).toBe(true);
    expect(screen.getByRole("button", { name: "Apply" })).toBeEnabled();
  });

  it("restores applied filters from browser history", async () => {
    const get = vi.fn().mockResolvedValue(authoritativeEnvelope([row("r1", "aws_s3_bucket.logs")]));
    const client = { get } as unknown as EshuApiClient;

    renderIacPage(
      <>
        <IacPage client={client} model={{ ...demoModel, iacResources: [] }} />
        <LocationProbe />
        <BackButton />
      </>,
      ["/iac?q=logs&kind=resource&repository=repository%3Ar1"],
    );

    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    expect(screen.getByLabelText<HTMLInputElement>("Search IaC resources").value).toBe("logs");
    expect(screen.getByLabelText<HTMLInputElement>("Filter by repository").value).toBe(
      "repository:r1",
    );

    fireEvent.change(screen.getByLabelText("Search IaC resources"), {
      target: { value: "archive" },
    });
    fireEvent.change(screen.getByLabelText("Filter by kind"), {
      target: { value: "data-source" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(screen.getByTestId("iac-location")).toHaveTextContent(
        "/iac?q=archive&kind=data-source&repository=repository%3Ar1",
      ),
    );
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));

    fireEvent.click(screen.getByRole("button", { name: "Browser back" }));

    await waitFor(() =>
      expect(screen.getByLabelText<HTMLInputElement>("Search IaC resources").value).toBe("logs"),
    );
    expect(screen.getByLabelText<HTMLSelectElement>("Filter by kind").value).toBe("resource");
    expect(screen.getByLabelText<HTMLInputElement>("Filter by repository").value).toBe(
      "repository:r1",
    );
    await waitFor(() => expect(get).toHaveBeenCalledTimes(3));
  });
});
