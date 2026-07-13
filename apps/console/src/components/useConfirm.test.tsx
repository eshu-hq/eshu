// components/useConfirm.test.tsx
import { useState } from "react";
import { describe, it, expect } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

import { useConfirm } from "./useConfirm";

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
