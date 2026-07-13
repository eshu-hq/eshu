// components/useConfirm.test.tsx
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { useState } from "react";
import { describe, it, expect } from "vitest";

import { useConfirm } from "./useConfirm";

// DoubleHost fires two confirm() calls back-to-back from a single handler so a
// test can assert the re-entrancy contract: opening a second dialog while the
// first is pending must resolve the first promise (as cancelled) rather than
// leak it forever.
function DoubleHost(): React.JSX.Element {
  const { confirm, confirmDialog } = useConfirm();
  const [log, setLog] = useState<string[]>([]);
  return (
    <div>
      <button
        type="button"
        onClick={() => {
          void confirm("first?").then((r) => setLog((l) => [...l, `first:${r}`]));
          void confirm("second?").then((r) => setLog((l) => [...l, `second:${r}`]));
        }}
      >
        OpenTwo
      </button>
      <span data-testid="log">{log.join(",")}</span>
      {confirmDialog}
    </div>
  );
}

// Host exercises the hook the way a panel does: a button opens the confirm,
// and the resolved boolean is written to visible text so tests can assert it.
function Host({ danger }: { readonly danger?: boolean }): React.JSX.Element {
  const { confirm, confirmDialog } = useConfirm();
  const [result, setResult] = useState<string>("idle");
  return (
    <div>
      <button
        type="button"
        onClick={async () => {
          const ok = await confirm("Delete the thing?", danger ? { danger: true } : {});
          setResult(ok ? "confirmed" : "cancelled");
        }}
      >
        Open
      </button>
      <span data-testid="result">{result}</span>
      {confirmDialog}
    </div>
  );
}

describe("useConfirm", () => {
  it("renders no dialog until confirm() is called", () => {
    render(<Host />);
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("resolves true when the confirm button is clicked", async () => {
    render(<Host />);
    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("Delete the thing?");
    // The prompt is announced to assistive tech: aria-describedby points at the
    // message element carrying the confirmation text.
    const describedBy = dialog.getAttribute("aria-describedby");
    expect(describedBy).toBeTruthy();
    expect(document.getElementById(describedBy!)).toHaveTextContent("Delete the thing?");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(screen.getByTestId("result")).toHaveTextContent("confirmed"));
    // Dialog closes after a decision.
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("resolves false when Cancel is clicked", async () => {
    render(<Host />);
    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    await screen.findByRole("alertdialog");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.getByTestId("result")).toHaveTextContent("cancelled"));
  });

  it("resolves false when Escape is pressed", async () => {
    render(<Host />);
    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    await screen.findByRole("alertdialog");
    fireEvent.keyDown(window, { key: "Escape" });
    await waitFor(() => expect(screen.getByTestId("result")).toHaveTextContent("cancelled"));
  });

  it("resolves false when the scrim is clicked", async () => {
    render(<Host />);
    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    await screen.findByRole("alertdialog");
    fireEvent.click(document.querySelector(".confirm-scrim")!);
    await waitFor(() => expect(screen.getByTestId("result")).toHaveTextContent("cancelled"));
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("resolves the prior promise as cancelled when a second confirm opens", async () => {
    render(<DoubleHost />);
    fireEvent.click(screen.getByRole("button", { name: "OpenTwo" }));
    // The first promise settles false immediately; the second dialog is shown.
    await waitFor(() => expect(screen.getByTestId("log")).toHaveTextContent("first:false"));
    expect(await screen.findByRole("alertdialog")).toHaveTextContent("second?");
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await waitFor(() =>
      expect(screen.getByTestId("log")).toHaveTextContent("first:false,second:true"),
    );
  });

  it("focuses the confirm button on open", async () => {
    render(<Host />);
    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    await screen.findByRole("alertdialog");
    await waitFor(() => expect(screen.getByRole("button", { name: "Confirm" })).toHaveFocus());
  });

  it("styles the confirm button as destructive when danger is set", async () => {
    render(<Host danger />);
    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    await screen.findByRole("alertdialog");
    expect(screen.getByRole("button", { name: "Confirm" })).toHaveClass("btn-danger");
  });
});
