// pages/admin/CopyField.test.tsx
// TDD coverage for the read-only copy-to-clipboard field (#4967) used by
// OidcProviderFields/SamlProviderFields for the endpoint URIs an operator
// registers with their IdP.
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, afterEach } from "vitest";

import { CopyField } from "./CopyField";

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe("CopyField", () => {
  it("renders the value read-only with the given label and aria-label", () => {
    render(
      <CopyField
        label="Redirect URI"
        value="https://eshu.example.test/callback"
        ariaLabel="OIDC redirect URI"
      />,
    );
    expect(screen.getByText("Redirect URI")).toBeInTheDocument();
    const input = screen.getByLabelText("OIDC redirect URI") as HTMLInputElement;
    expect(input.value).toBe("https://eshu.example.test/callback");
    expect(input).toHaveAttribute("readonly");
  });

  it("copies the value to the clipboard and shows Copied feedback", async () => {
    const writeText = vi.fn(async () => undefined);
    Object.defineProperty(globalThis.navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    render(
      <CopyField label="ACS URL" value="https://eshu.example.test/acs" ariaLabel="SAML ACS URL" />,
    );

    fireEvent.click(screen.getByRole("button", { name: /Copy/i }));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith("https://eshu.example.test/acs"));
    expect(await screen.findByRole("button", { name: /Copied/i })).toBeInTheDocument();
  });

  it("does not throw when the clipboard write is rejected", async () => {
    Object.defineProperty(globalThis.navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: vi.fn(async () => {
          throw new Error("denied");
        }),
      },
    });
    render(<CopyField label="Redirect URI" value="v" ariaLabel="OIDC redirect URI" />);
    fireEvent.click(screen.getByRole("button", { name: /Copy/i }));
    // No crash, and the button remains present (still says Copy, not stuck).
    expect(await screen.findByRole("button", { name: /Copy/i })).toBeInTheDocument();
  });
});
