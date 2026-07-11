// qrCode.ts — public entry point for the vendored byte-mode QR encoder
// (issue #5072). No new npm dependency was added for this: the encoder is a
// trimmed TypeScript port of the MIT-licensed "QR Code generator library"
// by Project Nayuki
// (https://www.nayuki.io/page/qr-code-generator-library,
// https://github.com/nayuki/QR-Code-generator, MIT License). See
// qrCodeEncoder.ts for the full attribution header, scope notes, and the
// module split that keeps every vendored file under the repo's 500-line
// cap.
import { buildQrMatrix } from "./qrCodeEncoder";

// encodeQrMatrix encodes `text` as a standard (non-Micro) QR symbol at
// error-correction level M, byte mode only, with automatic version (1-40)
// and mask (0-7) selection. Returns a square matrix where `true` is a dark
// module; the quiet zone is NOT included (callers such as qrMatrixToSvg add
// it separately). Throws if `text` is empty or too large to fit in a
// version-40 symbol at ECC level M.
export function encodeQrMatrix(text: string): boolean[][] {
  if (text.length === 0) {
    throw new Error("encodeQrMatrix: text must not be empty");
  }
  return buildQrMatrix(text);
}

export interface QrSvgOptions {
  // Side length of one module, in SVG user units. Defaults to 4.
  readonly moduleSize?: number;
  // Quiet-zone width, in modules, added on all four sides. The QR spec's
  // minimum is 4 modules; that is also this function's default.
  readonly quietZone?: number;
}

export interface QrSvgResult {
  // An SVG path `d` attribute drawing every dark module as a unit square,
  // offset by the quiet zone. Callers compose this into a <path> element
  // inside their own <svg viewBox="0 0 size size">.
  readonly path: string;
  // Total side length, in SVG user units, of the matrix plus quiet zone on
  // both sides — use this for the enclosing <svg>'s width/height/viewBox.
  readonly size: number;
}

// qrMatrixToSvg converts a dark/light module matrix (as returned by
// encodeQrMatrix) into an SVG path plus the total pixel size needed to
// render it with a standard quiet zone, so the React layer can stay
// declarative (build a plain <svg><path d={path} /></svg>).
export function qrMatrixToSvg(
  matrix: readonly (readonly boolean[])[],
  opts: QrSvgOptions = {},
): QrSvgResult {
  const moduleSize = opts.moduleSize ?? 4;
  const quietZone = opts.quietZone ?? 4;
  const dimension = matrix.length;
  if (dimension === 0) {
    throw new Error("qrMatrixToSvg: matrix must not be empty");
  }

  const segments: string[] = [];
  for (let y = 0; y < dimension; y++) {
    const row = matrix[y];
    for (let x = 0; x < dimension; x++) {
      if (row[x]) {
        const px = (x + quietZone) * moduleSize;
        const py = (y + quietZone) * moduleSize;
        segments.push(`M${px},${py}h${moduleSize}v${moduleSize}h${-moduleSize}z`);
      }
    }
  }

  return {
    path: segments.join(""),
    size: (dimension + quietZone * 2) * moduleSize,
  };
}
