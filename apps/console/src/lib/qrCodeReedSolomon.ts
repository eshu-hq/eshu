// qrCodeReedSolomon.ts — GF(256) Reed-Solomon arithmetic for QR Code error
// correction. Part of a trimmed, byte-mode-only TypeScript port of the
// public-domain "QR Code generator library" by Project Nayuki
// (https://www.nayuki.io/page/qr-code-generator-library,
// https://github.com/nayuki/QR-Code-generator, MIT License). See
// qrCodeEncoder.ts for the full attribution header and scope notes.
//
// The field is GF(2^8) with the QR spec's reducing polynomial
// x^8 + x^4 + x^3 + x^2 + 1 (0x11D), and generator element 0x02.

// reedSolomonMultiply multiplies two elements of GF(2^8) modulo the QR
// spec's reducing polynomial (ISO/IEC 18004 Annex A).
export function reedSolomonMultiply(x: number, y: number): number {
  let z = 0;
  for (let i = 7; i >= 0; i--) {
    z = (z << 1) ^ ((z >>> 7) * 0x11d);
    z ^= ((y >>> i) & 1) * x;
  }
  return z & 0xff;
}

// reedSolomonComputeDivisor computes the generator polynomial coefficients
// for a Reed-Solomon code with the given number of ECC codewords (degree).
export function reedSolomonComputeDivisor(degree: number): number[] {
  if (degree < 1 || degree > 255) {
    throw new RangeError(`reedSolomonComputeDivisor: degree out of range: ${degree}`);
  }
  const result: number[] = new Array<number>(degree).fill(0);
  result[degree - 1] = 1;

  let root = 1;
  for (let i = 0; i < degree; i++) {
    for (let j = 0; j < result.length; j++) {
      result[j] = reedSolomonMultiply(result[j], root);
      if (j + 1 < result.length) {
        result[j] ^= result[j + 1];
      }
    }
    root = reedSolomonMultiply(root, 0x02);
  }
  return result;
}

// reedSolomonComputeRemainder computes the ECC codewords for one data block
// against the given generator polynomial (divisor).
export function reedSolomonComputeRemainder(
  data: readonly number[],
  divisor: readonly number[],
): number[] {
  const result: number[] = divisor.map(() => 0);
  for (const b of data) {
    const factor = (b ^ (result.shift() as number)) & 0xff;
    result.push(0);
    divisor.forEach((coef, i) => {
      result[i] ^= reedSolomonMultiply(coef, factor);
    });
  }
  return result;
}
