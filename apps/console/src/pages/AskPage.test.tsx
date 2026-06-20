import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { AskPage } from "./AskPage";
import type { SourceState } from "../components/SourceControls";

function connectedSource(overrides: Partial<SourceState> = {}): SourceState {
  return { base: "https://eshu.example/api/", key: "shared", mode: "private", status: "connected", msg: "", ...overrides };
}

function probeResponse(state: string, reason = ""): Response {
  return new Response(JSON.stringify({ data: { state, reason }, error: null, truth: null }), {
    status: 200,
    headers: { "Content-Type": "application/json" }
  });
}

function sseResponse(chunks: readonly string[]): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk));
      }
      controller.close();
    }
  });
  return new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

function stubFetch(handler: (url: string, init?: RequestInit) => Response): void {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => handler(String(input), init))
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("AskPage", () => {
  it("streams reasoning steps and renders the evidence-backed answer", async () => {
    stubFetch((url) => {
      if (url.includes("/status/answer-narration")) {
        return probeResponse("available");
      }
      return sseResponse([
        'event: trace\ndata: {"tool":"resolve_entity","supported":true,"truth_class":"deterministic"}\n\n',
        'event: answer\ndata: {"answer_prose":"Checkout auth validates the session.","truth_class":"derived","limitations":["telemetry is 5h stale"],"partial":true}\n\n',
        "event: done\ndata: {}\n\n"
      ]);
    });

    render(
      <MemoryRouter>
        <AskPage source={connectedSource()} />
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText("Ask Eshu a question"), {
      target: { value: "How does checkout auth work?" }
    });
    fireEvent.click(screen.getByRole("button", { name: "Ask Eshu" }));

    expect(await screen.findByText("Checkout auth validates the session.")).toBeInTheDocument();
    expect(screen.getByText("resolve_entity")).toBeInTheDocument();
    expect(screen.getByText("This answer is partial.")).toBeInTheDocument();
    expect(screen.getByText("telemetry is 5h stale")).toBeInTheDocument();
    // The truth badge leads the answer.
    expect(screen.getByText("Derived")).toBeInTheDocument();
  });

  it("renders the disabled state when narration is disabled and hides the input", async () => {
    stubFetch((url) => {
      if (url.includes("/status/answer-narration")) {
        return probeResponse("disabled", "ESHU_ASK_ENABLED is unset");
      }
      throw new Error("ask should not be called when disabled");
    });

    render(
      <MemoryRouter>
        <AskPage source={connectedSource()} />
      </MemoryRouter>
    );

    expect(await screen.findByText("Ask Eshu is turned off")).toBeInTheDocument();
    expect(screen.getByText("ESHU_ASK_ENABLED is unset")).toBeInTheDocument();
    expect(screen.queryByLabelText("Ask Eshu a question")).not.toBeInTheDocument();
  });

  it("shows the evidence-only banner when narration is unavailable", async () => {
    stubFetch((url) => {
      if (url.includes("/status/answer-narration")) {
        return probeResponse("unavailable", "no provider profile");
      }
      return sseResponse(["event: done\ndata: {}\n\n"]);
    });

    render(
      <MemoryRouter>
        <AskPage source={connectedSource()} />
      </MemoryRouter>
    );

    expect(await screen.findByText(/evidence-only/i)).toBeInTheDocument();
    // Asking is still possible in this mode.
    expect(screen.getByLabelText("Ask Eshu a question")).toBeInTheDocument();
  });

  it("surfaces a scoped-token (403) error cleanly", async () => {
    stubFetch((url) => {
      if (url.includes("/status/answer-narration")) {
        return probeResponse("available");
      }
      return new Response("forbidden", { status: 403 });
    });

    render(
      <MemoryRouter>
        <AskPage source={connectedSource()} />
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText("Ask Eshu a question"), { target: { value: "anything" } });
    fireEvent.click(screen.getByRole("button", { name: "Ask Eshu" }));

    expect(await screen.findByText("This token can't ask")).toBeInTheDocument();
  });

  it("explains that demo mode has no live engine", () => {
    render(
      <MemoryRouter>
        <AskPage source={connectedSource({ mode: "demo", key: "" })} />
      </MemoryRouter>
    );

    expect(screen.getByText("Ask Eshu needs a live connection")).toBeInTheDocument();
    expect(screen.queryByLabelText("Ask Eshu a question")).not.toBeInTheDocument();
  });

  it("validates that a question is required before asking", async () => {
    stubFetch((url) => {
      if (url.includes("/status/answer-narration")) {
        return probeResponse("available");
      }
      throw new Error("ask should not be called for an empty question");
    });

    render(
      <MemoryRouter>
        <AskPage source={connectedSource()} />
      </MemoryRouter>
    );

    await screen.findByLabelText("Ask Eshu a question");
    fireEvent.click(screen.getByRole("button", { name: "Ask Eshu" }));
    expect(await screen.findByText("Type a question first.")).toBeInTheDocument();
  });
});
