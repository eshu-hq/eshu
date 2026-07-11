// qrCodeTables.ts — per-version constants and formulas for QR Code
// error-correction level M (Medium). Part of a trimmed, byte-mode-only
// TypeScript port of the MIT-licensed "QR Code generator library" by
// Project Nayuki (https://www.nayuki.io/page/qr-code-generator-library,
// https://github.com/nayuki/QR-Code-generator, MIT License). See
// qrCodeEncoder.ts for the full attribution header and scope notes.
//
// Only the ECC-M row of the spec's per-version tables is kept (issue #5072
// fixes error-correction level at Medium, so the L/Q/H rows are unused
// dead weight this port deliberately drops).

export const MIN_VERSION = 1;
export const MAX_VERSION = 40;

// Format-info bits for error-correction level "M" per ISO/IEC 18004 Table
// 25 (L=01, M=00, Q=11, H=10).
export const ECC_FORMAT_BITS_M = 0b00;

// Number of ECC codewords per block, indexed by version (index 0 unused).
const ECC_CODEWORDS_PER_BLOCK_M: readonly number[] = [
  -1, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26, 26, 28, 28,
  28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28,
];

// Number of error-correction blocks, indexed by version (index 0 unused).
const NUM_ERROR_CORRECTION_BLOCKS_M: readonly number[] = [
  -1, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16, 17, 17, 18, 20, 21, 23, 25,
  26, 28, 29, 31, 33, 35, 37, 38, 40, 43, 45, 47, 49,
];

// assertVersionInRange guards the per-version table accessors so an
// out-of-range version fails loudly (a RangeError) rather than silently
// returning the sentinel -1 or undefined and corrupting the ECC layout.
function assertVersionInRange(version: number, fn: string): void {
  if (version < MIN_VERSION || version > MAX_VERSION) {
    throw new RangeError(`${fn}: version out of range: ${version}`);
  }
}

export function eccCodewordsPerBlockM(version: number): number {
  assertVersionInRange(version, "eccCodewordsPerBlockM");
  return ECC_CODEWORDS_PER_BLOCK_M[version];
}

export function numErrorCorrectionBlocksM(version: number): number {
  assertVersionInRange(version, "numErrorCorrectionBlocksM");
  return NUM_ERROR_CORRECTION_BLOCKS_M[version];
}

// getNumRawDataModules returns the total number of data+ECC modules
// available in a QR symbol of the given version, before dividing into
// codewords (ISO/IEC 18004 formula, ignoring the format/version info areas
// which are excluded separately by the caller's bookkeeping).
export function getNumRawDataModules(version: number): number {
  if (version < MIN_VERSION || version > MAX_VERSION) {
    throw new RangeError(`getNumRawDataModules: version out of range: ${version}`);
  }
  let result = (16 * version + 128) * version + 64;
  if (version >= 2) {
    const numAlign = Math.floor(version / 7) + 2;
    result -= (25 * numAlign - 10) * numAlign - 55;
    if (version >= 7) {
      result -= 36;
    }
  }
  return result;
}

// getNumDataCodewordsM returns the number of data codewords (excluding
// ECC) available at error-correction level M for the given version.
export function getNumDataCodewordsM(version: number): number {
  return (
    Math.floor(getNumRawDataModules(version) / 8) -
    eccCodewordsPerBlockM(version) * numErrorCorrectionBlocksM(version)
  );
}

// getAlignmentPatternPositions returns the row/column coordinates at which
// alignment pattern centers are drawn for the given version (empty for
// version 1, which has none).
export function getAlignmentPatternPositions(version: number): number[] {
  if (version === 1) {
    return [];
  }
  const numAlign = Math.floor(version / 7) + 2;
  const step = version === 32 ? 26 : Math.ceil((version * 4 + 4) / (numAlign * 2 - 2)) * 2;
  const result: number[] = [6];
  for (let pos = version * 4 + 17 - 7; result.length < numAlign; pos -= step) {
    result.splice(1, 0, pos);
  }
  return result;
}

// byteModeCharCountBits returns the width, in bits, of the character-count
// field for a byte-mode segment at the given version (ISO/IEC 18004 Table
// 3: 8 bits for versions 1-9, 16 bits for versions 10-40).
export function byteModeCharCountBits(version: number): number {
  return version <= 9 ? 8 : 16;
}
