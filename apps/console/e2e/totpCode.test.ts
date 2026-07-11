// Unit tests for totpCode.ts's pure RFC 6238 TOTP math (issue #5073). These
// pin the implementation against the RFC 6238 Appendix B SHA1 known-answer
// vectors (the same vectors go/internal/totp/totp_test.go verifies the
// server side against) so a transcription bug in the dynamic-truncation or
// base32-decode math fails loudly here instead of surfacing as a mysterious
// "invalid" TOTP login rejection deep in the live E2E run.
import { describe, expect, it } from "vitest";

import { decodeBase32, generateTotpCode } from "./totpCode.ts";

// rfc6238Sha1Secret is the RFC 6238 Appendix B SHA1 test seed: the ASCII
// string "12345678901234567890", base32-encoded with no padding — exactly
// the shape go/internal/query/local_identity_totp.go's handleBeginTOTPEnrollment
// returns as the "secret" field.
const rfc6238Sha1Secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ";

describe("decodeBase32", () => {
  it("decodes the RFC 6238 test secret back to its raw ASCII bytes", () => {
    expect(decodeBase32(rfc6238Sha1Secret).toString("ascii")).toBe("12345678901234567890");
  });

  it("rejects a non-base32 character", () => {
    expect(() => decodeBase32("not-valid-base32-1")).toThrow(/invalid base32 character/);
  });
});

describe("generateTotpCode", () => {
  // RFC 6238 Appendix B, SHA1 rows, truncated to 6 digits (the server's
  // DefaultDigits): T=59 -> 94287082, T=1111111109 -> 07081804.
  it("matches the RFC 6238 vector at Unix time 59", () => {
    expect(generateTotpCode(rfc6238Sha1Secret, 59)).toBe("287082");
  });

  it("matches the RFC 6238 vector at Unix time 1111111109", () => {
    expect(generateTotpCode(rfc6238Sha1Secret, 1111111109)).toBe("081804");
  });

  it("changes once the 30s time step rolls over", () => {
    const first = generateTotpCode(rfc6238Sha1Secret, 1111111109);
    const next = generateTotpCode(rfc6238Sha1Secret, 1111111109 + 30);
    expect(next).not.toBe(first);
  });
});
