// qrCodeEncoder.ts — byte-mode-only QR Code matrix builder (issue #5072:
// scannable QR for TOTP enrollment, replacing text-only otpauth:// display
// from #4986).
//
// This is a trimmed TypeScript port of the "QR Code generator library" by
// Project Nayuki (https://www.nayuki.io/page/qr-code-generator-library,
// https://github.com/nayuki/QR-Code-generator), adapted from its reference
// algorithm (ISO/IEC 18004). The upstream library is MIT-licensed; its
// notice is retained here as required:
//
//   MIT License. Copyright (c) Project Nayuki.
//   Permission is hereby granted, free of charge, to any person obtaining a
//   copy of this software and associated documentation files (the
//   "Software"), to deal in the Software without restriction, including
//   without limitation the rights to use, copy, modify, merge, publish,
//   distribute, sublicense, and/or sell copies of the Software, and to permit
//   persons to whom the Software is furnished to do so, subject to the
//   following conditions: The above copyright notice and this permission
//   notice shall be included in all copies or substantial portions of the
//   Software. The Software is provided "as is", without warranty of any kind,
//   express or implied, including but not limited to the warranties of
//   merchantability, fitness for a particular purpose and noninfringement. In
//   no event shall the authors or copyright holders be liable for any claim,
//   damages or other liability, whether in an action of contract, tort or
//   otherwise, arising from, out of or in connection with the Software or the
//   use or other dealings in the Software.
//
// Deliberately dropped relative to
// the upstream library, because this call site only ever encodes an
// otpauth:// URI:
//   - Segment modes: byte mode only (no numeric/alphanumeric/kanji/ECI).
//   - Error-correction level: fixed at M (no L/Q/H, no boost-ECL step).
//   - Symbol family: standard QR only (no Micro QR).
//   - Mask selection: always automatic (no manual mask override).
// Version selection (1-40) and mask selection (0-7, by the standard
// penalty score) remain fully automatic, matching the reference algorithm.
//
// Split across qrCodeEncoderState.ts (mutable grid), qrCodeReedSolomon.ts
// (GF(256) arithmetic), qrCodeTables.ts (per-version ECC-M constants), and
// qrCodeMasking.ts (masking + penalty scoring) to keep each file well under
// the repo's 500-line cap. qrCode.ts is the public entry point.
import { newQrMatrixState, setFunctionModule, type QrMatrixState } from "./qrCodeEncoderState";
import { applyMask, getPenaltyScore } from "./qrCodeMasking";
import { reedSolomonComputeDivisor, reedSolomonComputeRemainder } from "./qrCodeReedSolomon";
import {
  MAX_VERSION,
  MIN_VERSION,
  byteModeCharCountBits,
  eccCodewordsPerBlockM,
  getAlignmentPatternPositions,
  getNumDataCodewordsM,
  getNumRawDataModules,
  numErrorCorrectionBlocksM,
  ECC_FORMAT_BITS_M,
} from "./qrCodeTables";

const BYTE_MODE_BITS = 0b0100;

function getBit(value: number, i: number): boolean {
  return ((value >>> i) & 1) !== 0;
}

function appendBits(value: number, len: number, bb: number[]): void {
  for (let i = len - 1; i >= 0; i--) {
    bb.push((value >>> i) & 1);
  }
}

// selectVersionAndBuildData picks the smallest QR version (at ECC level M)
// that fits the UTF-8 bytes of `text` as a single byte-mode segment, then
// builds the padded data-codeword bit stream for that version.
function selectVersionAndBuildData(text: string): { version: number; dataCodewords: number[] } {
  const bytes = Array.from(new TextEncoder().encode(text));
  if (bytes.length === 0) {
    throw new Error("encodeQrMatrix: text must not be empty");
  }

  let version = -1;
  for (let v = MIN_VERSION; v <= MAX_VERSION; v++) {
    const ccBits = byteModeCharCountBits(v);
    if (bytes.length >= 1 << ccBits) continue;
    const capacityBits = getNumDataCodewordsM(v) * 8;
    const usedBits = 4 + ccBits + bytes.length * 8;
    if (usedBits <= capacityBits) {
      version = v;
      break;
    }
  }
  if (version < 0) {
    throw new RangeError(
      "encodeQrMatrix: text too long to fit in a version-40 QR code at error-correction level M",
    );
  }

  const bb: number[] = [];
  appendBits(BYTE_MODE_BITS, 4, bb);
  appendBits(bytes.length, byteModeCharCountBits(version), bb);
  for (const b of bytes) appendBits(b, 8, bb);

  const capacityBits = getNumDataCodewordsM(version) * 8;
  appendBits(0, Math.min(4, capacityBits - bb.length), bb);
  // Pad to the next codeword (byte) boundary. This intentionally matches
  // the golden oracle's exact behavior (the pure-Python `segno` v1.6.6
  // reference encoder's write_padding_bits, see qrCode.test.ts) rather than
  // the tighter `(8 - bitLen % 8) % 8` reading of ISO/IEC 18004 Section
  // 7.4.10: when the terminator already lands the bit stream on a byte
  // boundary, segno still emits one full extra zero byte instead of zero
  // padding bits. The resulting symbol is still valid and scans correctly
  // (a decoder reads exactly `char_count` content bytes per the segment
  // header, then treats everything else — extra zero bits, the alternating
  // 0xEC/0x11 pad codewords — as filler), so replicating the oracle's exact
  // byte layout here is what makes automatic version/mask selection and the
  // final matrix agree with it bit-for-bit. Clamped to capacityBits so a
  // pathological input that fills the symbol exactly at a byte boundary
  // cannot overflow the codeword buffer.
  appendBits(0, Math.min(8 - (bb.length % 8), capacityBits - bb.length), bb);
  for (let padByte = 0xec; bb.length < capacityBits; padByte ^= 0xec ^ 0x11) {
    appendBits(padByte, 8, bb);
  }

  const dataCodewords: number[] = new Array<number>(bb.length / 8).fill(0);
  bb.forEach((bit, i) => {
    dataCodewords[i >>> 3] |= bit << (7 - (i & 7));
  });

  return { version, dataCodewords };
}

// addEccAndInterleave splits the data codewords into the version's blocks,
// computes each block's Reed-Solomon ECC codewords, and interleaves data
// and ECC codewords across blocks per ISO/IEC 18004 Section 8.7.3.
function addEccAndInterleave(version: number, data: readonly number[]): number[] {
  const numBlocks = numErrorCorrectionBlocksM(version);
  const blockEccLen = eccCodewordsPerBlockM(version);
  const rawCodewords = Math.floor(getNumRawDataModules(version) / 8);
  const numShortBlocks = numBlocks - (rawCodewords % numBlocks);
  const shortBlockLen = Math.floor(rawCodewords / numBlocks);

  const rsDiv = reedSolomonComputeDivisor(blockEccLen);
  const blocks: number[][] = [];
  let k = 0;
  for (let i = 0; i < numBlocks; i++) {
    const takeLen = shortBlockLen - blockEccLen + (i < numShortBlocks ? 0 : 1);
    const dat = data.slice(k, k + takeLen);
    k += dat.length;
    const ecc = reedSolomonComputeRemainder(dat, rsDiv);
    if (i < numShortBlocks) {
      dat.push(0);
    }
    blocks.push([...dat, ...ecc]);
  }

  const result: number[] = [];
  const blockLen = blocks[0].length;
  for (let i = 0; i < blockLen; i++) {
    for (let j = 0; j < blocks.length; j++) {
      if (i !== shortBlockLen - blockEccLen || j >= numShortBlocks) {
        result.push(blocks[j][i]);
      }
    }
  }
  return result;
}

function drawFinderPattern(state: QrMatrixState, x: number, y: number): void {
  for (let dy = -4; dy <= 4; dy++) {
    for (let dx = -4; dx <= 4; dx++) {
      const dist = Math.max(Math.abs(dx), Math.abs(dy));
      const xx = x + dx;
      const yy = y + dy;
      if (xx >= 0 && xx < state.size && yy >= 0 && yy < state.size) {
        setFunctionModule(state, xx, yy, dist !== 2 && dist !== 4);
      }
    }
  }
}

function drawAlignmentPattern(state: QrMatrixState, x: number, y: number): void {
  for (let dy = -2; dy <= 2; dy++) {
    for (let dx = -2; dx <= 2; dx++) {
      setFunctionModule(state, x + dx, y + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1);
    }
  }
}

// reserveFormatInfoModules marks the format-info field's module positions
// as function modules WITHOUT writing real bits yet (they stay light/false,
// mirroring how the reference segno oracle reserves this area as blank
// during mask evaluation). Format info encodes the chosen mask, so its real
// value cannot be known before a mask is selected; per ISO/IEC 18004
// Section 8.8.2, mask selection must be scored against the symbol WITHOUT
// format info skewing the penalty count, only reserving the positions so
// codeword placement and masking skip them. drawFormatBits fills in the
// real bits once the best mask is chosen.
function reserveFormatInfoModules(state: QrMatrixState): void {
  for (let i = 0; i <= 5; i++) setFunctionModule(state, 8, i, false);
  setFunctionModule(state, 8, 7, false);
  setFunctionModule(state, 8, 8, false);
  setFunctionModule(state, 7, 8, false);
  for (let i = 9; i < 15; i++) setFunctionModule(state, 14 - i, 8, false);

  for (let i = 0; i < 8; i++) setFunctionModule(state, state.size - 1 - i, 8, false);
  for (let i = 8; i < 15; i++) setFunctionModule(state, 8, state.size - 15 + i, false);
  setFunctionModule(state, 8, state.size - 8, false);
}

// drawFormatBits draws the real 15-bit format-info field (ECC level + mask,
// BCH-protected) in its two redundant locations, for the finally-chosen
// mask. Must run after mask selection (see reserveFormatInfoModules).
function drawFormatBits(state: QrMatrixState, mask: number): void {
  const data = (ECC_FORMAT_BITS_M << 3) | mask;
  let rem = data;
  for (let i = 0; i < 10; i++) {
    rem = (rem << 1) ^ ((rem >>> 9) * 0x537);
  }
  const bits = ((data << 10) | rem) ^ 0x5412;

  for (let i = 0; i <= 5; i++) setFunctionModule(state, 8, i, getBit(bits, i));
  setFunctionModule(state, 8, 7, getBit(bits, 6));
  setFunctionModule(state, 8, 8, getBit(bits, 7));
  setFunctionModule(state, 7, 8, getBit(bits, 8));
  for (let i = 9; i < 15; i++) setFunctionModule(state, 14 - i, 8, getBit(bits, i));

  for (let i = 0; i < 8; i++) setFunctionModule(state, state.size - 1 - i, 8, getBit(bits, i));
  for (let i = 8; i < 15; i++) setFunctionModule(state, 8, state.size - 15 + i, getBit(bits, i));
  setFunctionModule(state, 8, state.size - 8, true);
}

// reserveVersionInfoModules marks the version-info field's module positions
// (version 7+ only) as function modules without writing real bits yet, for
// the same reason as reserveFormatInfoModules: keep the trial-mask penalty
// score from being skewed before the real value is drawn.
function reserveVersionInfoModules(state: QrMatrixState, version: number): void {
  if (version < 7) return;
  for (let i = 0; i < 18; i++) {
    const a = state.size - 11 + (i % 3);
    const b = Math.floor(i / 3);
    setFunctionModule(state, a, b, false);
    setFunctionModule(state, b, a, false);
  }
}

// drawVersionInfo draws the real 18-bit version-info field (version 7+
// only, BCH-protected). Version info does not depend on the chosen mask,
// but is drawn after mask selection to match reserveVersionInfoModules.
function drawVersionInfo(state: QrMatrixState, version: number): void {
  if (version < 7) return;
  let rem = version;
  for (let i = 0; i < 12; i++) {
    rem = (rem << 1) ^ ((rem >>> 11) * 0x1f25);
  }
  const bits = (version << 12) | rem;
  for (let i = 0; i < 18; i++) {
    const color = getBit(bits, i);
    const a = state.size - 11 + (i % 3);
    const b = Math.floor(i / 3);
    setFunctionModule(state, a, b, color);
    setFunctionModule(state, b, a, color);
  }
}

function drawFunctionPatterns(state: QrMatrixState, version: number): void {
  for (let i = 0; i < state.size; i++) {
    setFunctionModule(state, 6, i, i % 2 === 0);
    setFunctionModule(state, i, 6, i % 2 === 0);
  }

  drawFinderPattern(state, 3, 3);
  drawFinderPattern(state, state.size - 4, 3);
  drawFinderPattern(state, 3, state.size - 4);

  const alignPos = getAlignmentPatternPositions(version);
  const n = alignPos.length;
  for (let i = 0; i < n; i++) {
    for (let j = 0; j < n; j++) {
      const isCorner = (i === 0 && j === 0) || (i === 0 && j === n - 1) || (i === n - 1 && j === 0);
      if (!isCorner) drawAlignmentPattern(state, alignPos[i], alignPos[j]);
    }
  }

  reserveFormatInfoModules(state);
  reserveVersionInfoModules(state, version);
}

// drawCodewords places the interleaved data+ECC codeword bits into every
// non-function module, in the zigzag column order defined by ISO/IEC 18004
// Section 8.7.4 (two-module-wide columns, right to left, alternating
// vertical direction, skipping the vertical timing column).
function drawCodewords(state: QrMatrixState, data: readonly number[]): void {
  let i = 0;
  for (let right = state.size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5;
    for (let vert = 0; vert < state.size; vert++) {
      for (let j = 0; j < 2; j++) {
        const x = right - j;
        const upward = ((right + 1) & 2) === 0;
        const y = upward ? state.size - 1 - vert : vert;
        if (!state.isFunctionModule[y][x] && i < data.length * 8) {
          state.modules[y][x] = getBit(data[i >>> 3], 7 - (i & 7));
          i++;
        }
      }
    }
  }
}

// buildQrMatrix encodes `text` as a single byte-mode segment at
// error-correction level M, choosing the smallest version that fits and
// the mask (0-7) with the lowest standard penalty score, per ISO/IEC
// 18004.
export function buildQrMatrix(text: string): boolean[][] {
  const { version, dataCodewords } = selectVersionAndBuildData(text);
  const allCodewords = addEccAndInterleave(version, dataCodewords);

  const state = newQrMatrixState(version);
  drawFunctionPatterns(state, version);
  drawCodewords(state, allCodewords);

  // Score each candidate mask on the symbol as placed so far (finder/
  // timing/alignment patterns + masked data, format/version info still
  // blank/reserved) — format info is not drawn during evaluation because it
  // encodes the mask being chosen, so including it would bias the score.
  let bestMask = 0;
  let bestPenalty = Number.POSITIVE_INFINITY;
  for (let mask = 0; mask < 8; mask++) {
    applyMask(state, mask);
    const penalty = getPenaltyScore(state);
    if (penalty < bestPenalty) {
      bestPenalty = penalty;
      bestMask = mask;
    }
    applyMask(state, mask); // undo the trial mask before trying the next one
  }
  applyMask(state, bestMask);
  drawFormatBits(state, bestMask);
  drawVersionInfo(state, version);

  return state.modules;
}
