import * as React from "react";
import { version as reactDomVersion } from "react-dom";
import { describe, expect, it } from "vitest";

// Guard for the dependency contract behind #3103. @testing-library/react v16
// resolves its act() implementation at module load with
// `typeof React.act === 'function' ? React.act : <deprecated react-dom/test-utils>`.
// When a drifted install resolves react < 19 (no React.act) or a second,
// mismatched react/react-dom copy, that fallback fires and rendering blows up
// deep inside RTL with the opaque `React.act is not a function`. These
// assertions surface the real cause early and legibly instead.
describe("React test environment contract (#3103)", () => {
  it("exposes the React 19 act() API that @testing-library/react relies on", () => {
    expect(typeof React.act).toBe("function");
  });

  it("loads aligned react/react-dom majors (no mismatched copies)", () => {
    const reactMajor = React.version.split(".")[0];
    const reactDomMajor = reactDomVersion.split(".")[0];

    expect(reactMajor).toBe("19");
    expect(reactDomMajor).toBe("19");
    // Major alignment, not exact lockstep: independent patch bumps (e.g. a
    // Dependabot update of one package) are valid and must not fail the gate.
    expect(reactMajor).toBe(reactDomMajor);
  });
});
