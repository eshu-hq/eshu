// qrCodeEncoderState.ts — the mutable module grid shared by qrCodeEncoder.ts
// (function-pattern + codeword drawing) and qrCodeMasking.ts (masking +
// penalty scoring), split out to avoid a circular import between the two.
// Part of a trimmed, byte-mode-only TypeScript port of the public-domain
// "QR Code generator library" by Project Nayuki
// (https://www.nayuki.io/page/qr-code-generator-library,
// https://github.com/nayuki/QR-Code-generator, MIT License). See
// qrCodeEncoder.ts for the full attribution header and scope notes.

export interface QrMatrixState {
  readonly size: number;
  readonly modules: boolean[][];
  readonly isFunctionModule: boolean[][];
}

// newQrMatrixState allocates an all-light, all-non-function size x size
// grid for the given QR version (size = version * 4 + 17 per ISO/IEC
// 18004).
export function newQrMatrixState(version: number): QrMatrixState {
  const size = version * 4 + 17;
  const modules: boolean[][] = [];
  const isFunctionModule: boolean[][] = [];
  for (let i = 0; i < size; i++) {
    modules.push(new Array<boolean>(size).fill(false));
    isFunctionModule.push(new Array<boolean>(size).fill(false));
  }
  return { size, modules, isFunctionModule };
}

// setFunctionModule sets a module that belongs to a fixed structural
// pattern (finder, timing, alignment, format/version info) rather than to
// encoded data, so the codeword-placement and masking passes skip it.
export function setFunctionModule(
  state: QrMatrixState,
  x: number,
  y: number,
  isDark: boolean,
): void {
  state.modules[y][x] = isDark;
  state.isFunctionModule[y][x] = true;
}
