// totpCode.ts — pure RFC 6238 TOTP code generator for the browser-auth E2E
// harness (issue #5073, Flow B: a LOCAL member enrolls TOTP via POST
// /api/v0/auth/local/mfa/totp/begin then must prove a live code at POST
// /api/v0/auth/local/mfa/totp/confirm and POST /api/v0/auth/local/login).
// There is no TOTP generator anywhere else in this harness, and the task
// forbids a new npm dependency, so this reimplements the RFC 4226/6238 math
// with node:crypto's HMAC only — deliberately mirroring
// go/internal/totp/totp.go's hotp() byte-for-byte (HMAC-SHA1, RFC 4226 §5.3
// dynamic truncation) and go/internal/query/local_identity_totp.go's
// base32.StdEncoding.WithPadding(base32.NoPadding) secret encoding, so this
// client-side code computes the exact same code the server's
// go/internal/totp.Verify would accept for the same secret/time/step/digits.
import { createHmac } from "node:crypto";

const base32Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

// decodeBase32 decodes an RFC 4648 §6 base32 string (case-insensitive,
// padding optional/stripped) into raw bytes. This is the exact inverse of
// the server's base32.StdEncoding.WithPadding(base32.NoPadding) encoding
// returned by POST .../mfa/totp/begin's "secret" field.
export function decodeBase32(input: string): Buffer {
  const clean = input.trim().toUpperCase().replace(/=+$/g, "");
  let bits = 0;
  let value = 0;
  const bytes: number[] = [];
  for (const char of clean) {
    const idx = base32Alphabet.indexOf(char);
    if (idx === -1) {
      throw new Error(`invalid base32 character in TOTP secret: ${char}`);
    }
    value = (value << 5) | idx;
    bits += 5;
    if (bits >= 8) {
      bits -= 8;
      bytes.push((value >>> bits) & 0xff);
    }
  }
  return Buffer.from(bytes);
}

// generateTotpCode computes the RFC 6238 TOTP code for a base32-encoded
// secret at unixTimeSeconds: RFC 4226 §5.2 HOTP over the RFC 6238 §4.2
// time-step counter floor(unixTimeSeconds / periodSeconds), then RFC 4226
// §5.3 dynamic truncation over the low 4 bits of the last HMAC byte.
// digits/periodSeconds default to the server's own DefaultDigits (6) /
// DefaultStep (30s) — see go/internal/totp/totp.go.
export function generateTotpCode(
  secretBase32: string,
  unixTimeSeconds: number,
  digits = 6,
  periodSeconds = 30,
): string {
  const counter = Math.floor(unixTimeSeconds / periodSeconds);
  // Counter is a 64-bit big-endian integer per RFC 4226 §5.2. Splitting into
  // two 32-bit halves keeps this within safe-integer bitwise math for any
  // realistic Unix time (a single 32-bit half does not overflow until well
  // past the year 2106).
  const counterBuf = Buffer.alloc(8);
  counterBuf.writeUInt32BE(Math.floor(counter / 0x100000000), 0);
  counterBuf.writeUInt32BE(counter >>> 0, 4);

  const secret = decodeBase32(secretBase32);
  const hmac = createHmac("sha1", secret).update(counterBuf).digest();
  const offset = hmac[hmac.length - 1]! & 0x0f;
  const truncated =
    ((hmac[offset]! & 0x7f) << 24) |
    ((hmac[offset + 1]! & 0xff) << 16) |
    ((hmac[offset + 2]! & 0xff) << 8) |
    (hmac[offset + 3]! & 0xff);
  return (truncated % 10 ** digits).toString().padStart(digits, "0");
}
