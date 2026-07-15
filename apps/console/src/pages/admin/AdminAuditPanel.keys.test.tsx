import { render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AdminAuditPanel, auditEventKeys } from "./AdminAuditPanel";
import type { EshuApiClient } from "../../api/client";

const NOW = "2026-06-24T10:00:00Z";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("AdminAuditPanel row identity", () => {
  it("renders duplicate and missing correlation ids once without duplicate React keys", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const client = {
      getJson: async (path: string) =>
        path.endsWith("/audit/summary")
          ? { total: 4, allowed: 4, denied: 0, unavailable: 0, last_occurred_at: NOW }
          : {
              events: [
                { correlation_id: "shared", event_type: "first", occurred_at: NOW },
                { correlation_id: "shared", event_type: "second", occurred_at: NOW },
                { correlation_id: null, event_type: "third", occurred_at: NOW },
                { event_type: "fourth", occurred_at: NOW },
              ],
            },
    } as unknown as EshuApiClient;

    render(<AdminAuditPanel client={client} />);

    const table = await screen.findByRole("table", { name: "Audit events" });
    expect(within(table).getAllByRole("row")).toHaveLength(5);
    for (const eventType of ["first", "second", "third", "fourth"]) {
      expect(within(table).getAllByText(eventType)).toHaveLength(1);
    }
    expect(
      consoleError.mock.calls.some((call) =>
        call.some((value) => String(value).includes("same key")),
      ),
    ).toBe(false);
  });

  it("renders exact duplicate audit records without duplicate React keys", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const duplicate = { correlation_id: "shared", event_type: "same", occurred_at: NOW };
    const client = {
      getJson: async (path: string) =>
        path.endsWith("/audit/summary") ? { total: 2 } : { events: [duplicate, duplicate] },
    } as unknown as EshuApiClient;

    render(<AdminAuditPanel client={client} />);

    const table = await screen.findByRole("table", { name: "Audit events" });
    expect(within(table).getAllByText("same")).toHaveLength(2);
    expect(
      consoleError.mock.calls.some((call) =>
        call.some((value) => String(value).includes("same key")),
      ),
    ).toBe(false);
  });

  it("builds stable audit keys in one pass across a reorder", () => {
    const first = { correlation_id: "shared", event_type: "first", occurred_at: NOW };
    const second = { correlation_id: "shared", event_type: "second", occurred_at: NOW };

    expect(new Set(auditEventKeys([first, second]))).toEqual(
      new Set(auditEventKeys([second, first])),
    );
  });
});
