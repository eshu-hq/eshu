// qrCodeMasking.ts — data-mask application and the standard penalty-based
// automatic mask selection scoring (ISO/IEC 18004 Section 8.8.2, Table 11:
// rules N1-N4). Part of a trimmed, byte-mode-only TypeScript port of the
// public-domain "QR Code generator library" by Project Nayuki
// (https://www.nayuki.io/page/qr-code-generator-library,
// https://github.com/nayuki/QR-Code-generator, MIT License). See
// qrCodeEncoder.ts for the full attribution header and scope notes.
//
// The N3 (finder-like-pattern) rule below is intentionally NOT the
// run-history state machine from Nayuki's reference — it is a direct port
// of the equivalent substring-search approach used by the pure-Python
// `segno` library (github.com/heuer/segno, MIT License), which issue #5072
// uses as the independent oracle for the golden test (qrCode.test.ts). Both
// approaches implement the same ISO/IEC 18004 rule; matching segno's exact
// algorithm (rather than a differently-shaped but spec-equivalent one) is
// what makes automatic mask selection agree with the oracle bit-for-bit.
import type { QrMatrixState } from "./qrCodeEncoderState";

// N1 (base penalty 3, +1 per module beyond the 5-run threshold) is folded
// directly into rowRunPenalty's `run - 2` formula below rather than kept as
// a named constant multiplied in.
const PENALTY_N2 = 3;
const PENALTY_N3 = 40;
const PENALTY_N4 = 10;

// maskPredicate implements the eight standard QR data-mask formulas
// (ISO/IEC 18004 Table 10). Returns true where mask index `mask` inverts
// the module at (x, y).
function maskPredicate(mask: number, x: number, y: number): boolean {
  switch (mask) {
    case 0:
      return (x + y) % 2 === 0;
    case 1:
      return y % 2 === 0;
    case 2:
      return x % 3 === 0;
    case 3:
      return (x + y) % 3 === 0;
    case 4:
      return (Math.floor(x / 3) + Math.floor(y / 2)) % 2 === 0;
    case 5:
      return ((x * y) % 2) + ((x * y) % 3) === 0;
    case 6:
      return (((x * y) % 2) + ((x * y) % 3)) % 2 === 0;
    case 7:
      return (((x + y) % 2) + ((x * y) % 3)) % 2 === 0;
    default:
      throw new RangeError(`maskPredicate: invalid mask index: ${mask}`);
  }
}

// applyMask XORs the given mask over every non-function module. Calling it
// twice with the same mask index restores the original modules (used by
// the caller to trial each of the 8 masks before committing to the best).
export function applyMask(state: QrMatrixState, mask: number): void {
  for (let y = 0; y < state.size; y++) {
    for (let x = 0; x < state.size; x++) {
      if (!state.isFunctionModule[y][x] && maskPredicate(mask, x, y)) {
        state.modules[y][x] = !state.modules[y][x];
      }
    }
  }
}

// rowRunPenalty (N1) sums (runLength - 2) for every run of 5+ same-colored
// modules along one row or column (already-extracted as `line`).
function rowRunPenalty(line: readonly boolean[]): number {
  let total = 0;
  let run = 1;
  for (let i = 1; i < line.length; i++) {
    if (line[i] === line[i - 1]) {
      run++;
    } else {
      if (run >= 5) total += run - 2;
      run = 1;
    }
  }
  if (run >= 5) total += run - 2;
  return total;
}

const N3_PATTERN: readonly boolean[] = [true, false, true, true, true, false, true];

function matchesN3PatternAt(line: readonly boolean[], idx: number): boolean {
  for (let k = 0; k < N3_PATTERN.length; k++) {
    if (line[idx + k] !== N3_PATTERN[k]) return false;
  }
  return true;
}

function findN3Pattern(line: readonly boolean[], from: number): number {
  for (let i = Math.max(from, 0); i <= line.length - N3_PATTERN.length; i++) {
    if (matchesN3PatternAt(line, i)) return i;
  }
  return -1;
}

// anyDark reports whether any module in line[from, to) is dark, clamped to
// the line's bounds. An empty (or out-of-bounds) range reports false, same
// as Python's `any(())`.
function anyDark(line: readonly boolean[], from: number, to: number): boolean {
  const start = Math.max(from, 0);
  const end = Math.min(to, line.length);
  for (let i = start; i < end; i++) {
    if (line[i]) return true;
  }
  return false;
}

// finderLikePatternPenalty (N3) finds every occurrence of the 1:1:3:1:1
// dark:light:dark:dark:dark:light:dark run along one row or column, and
// penalizes it when it sits at the symbol's edge or has 4+ light modules
// immediately before or after it (so it cannot be mistaken for part of an
// actual finder pattern). A non-qualifying match still advances the search
// by only 4 (not 7) so overlapping candidate patterns are not missed.
function finderLikePatternPenalty(line: readonly boolean[]): number {
  const size = line.length;
  let count = 0;
  let idx = findN3Pattern(line, 0);
  while (idx !== -1) {
    let advanceTo = idx + N3_PATTERN.length;
    const atEdge = idx === 0 || idx === size - N3_PATTERN.length;
    const lightBefore = !anyDark(line, idx - 4, idx);
    const lightAfter = !anyDark(line, advanceTo, advanceTo + 4);
    if (atEdge || lightBefore || lightAfter) {
      count += PENALTY_N3;
    } else {
      advanceTo = idx + 4;
    }
    idx = findN3Pattern(line, advanceTo);
  }
  return count;
}

function getColumn(modules: readonly (readonly boolean[])[], size: number, x: number): boolean[] {
  const col: boolean[] = new Array<boolean>(size);
  for (let y = 0; y < size; y++) col[y] = modules[y][x];
  return col;
}

// getPenaltyScore computes the ISO/IEC 18004 penalty score (rules N1-N4)
// used to automatically pick the mask that yields the most scan-friendly
// symbol. Lower is better; the caller tries all 8 masks and keeps the min.
export function getPenaltyScore(state: QrMatrixState): number {
  const { size, modules } = state;
  let result = 0;

  for (let y = 0; y < size; y++) {
    result += rowRunPenalty(modules[y]);
    result += finderLikePatternPenalty(modules[y]);
  }
  for (let x = 0; x < size; x++) {
    const col = getColumn(modules, size, x);
    result += rowRunPenalty(col);
    result += finderLikePatternPenalty(col);
  }

  // N2: 2x2 blocks of same-colored modules.
  for (let y = 0; y < size - 1; y++) {
    for (let x = 0; x < size - 1; x++) {
      const color = modules[y][x];
      if (
        color === modules[y][x + 1] &&
        color === modules[y + 1][x] &&
        color === modules[y + 1][x + 1]
      ) {
        result += PENALTY_N2;
      }
    }
  }

  // N4: proportion of dark modules, penalized the further it strays from
  // 50%, in 5-percentage-point steps.
  let dark = 0;
  for (const row of modules) {
    for (const cell of row) {
      if (cell) dark++;
    }
  }
  const total = size * size;
  const k = Math.floor(Math.abs(dark * 20 - total * 10) / total);
  result += k * PENALTY_N4;

  return result;
}
