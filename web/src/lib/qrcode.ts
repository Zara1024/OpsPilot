// Pure TypeScript ISO/IEC 18004 QR Code encoder (Byte mode, ECC-M, Versions 1..12).
// Keeps the bundle free of third-party QR dependencies while rendering crisp SVG codes
// suitable for Google Authenticator / 1Password / Microsoft Authenticator TOTP enrollment.

type VersionSpec = {
  version: number;
  dataCodewords: number;
  ecPerBlock: number;
  g1Blocks: number;
  g1Data: number;
  g2Blocks: number;
  g2Data: number;
  alignment: number[];
};

// Error Correction Level M parameters (ISO/IEC 18004 Table 9), versions 1..12.
const VERSIONS_M: VersionSpec[] = [
  { version: 1, dataCodewords: 16, ecPerBlock: 10, g1Blocks: 1, g1Data: 16, g2Blocks: 0, g2Data: 0, alignment: [] },
  { version: 2, dataCodewords: 28, ecPerBlock: 16, g1Blocks: 1, g1Data: 28, g2Blocks: 0, g2Data: 0, alignment: [6, 18] },
  { version: 3, dataCodewords: 44, ecPerBlock: 26, g1Blocks: 1, g1Data: 44, g2Blocks: 0, g2Data: 0, alignment: [6, 22] },
  { version: 4, dataCodewords: 64, ecPerBlock: 18, g1Blocks: 2, g1Data: 32, g2Blocks: 0, g2Data: 0, alignment: [6, 26] },
  { version: 5, dataCodewords: 86, ecPerBlock: 24, g1Blocks: 2, g1Data: 43, g2Blocks: 0, g2Data: 0, alignment: [6, 30] },
  { version: 6, dataCodewords: 108, ecPerBlock: 16, g1Blocks: 4, g1Data: 27, g2Blocks: 0, g2Data: 0, alignment: [6, 34] },
  { version: 7, dataCodewords: 124, ecPerBlock: 18, g1Blocks: 4, g1Data: 31, g2Blocks: 0, g2Data: 0, alignment: [6, 22, 38] },
  { version: 8, dataCodewords: 154, ecPerBlock: 22, g1Blocks: 2, g1Data: 38, g2Blocks: 2, g2Data: 39, alignment: [6, 24, 42] },
  { version: 9, dataCodewords: 182, ecPerBlock: 22, g1Blocks: 3, g1Data: 36, g2Blocks: 2, g2Data: 37, alignment: [6, 26, 46] },
  { version: 10, dataCodewords: 216, ecPerBlock: 26, g1Blocks: 4, g1Data: 43, g2Blocks: 1, g2Data: 44, alignment: [6, 28, 50] },
  { version: 11, dataCodewords: 254, ecPerBlock: 30, g1Blocks: 1, g1Data: 50, g2Blocks: 4, g2Data: 51, alignment: [6, 30, 54] },
  { version: 12, dataCodewords: 290, ecPerBlock: 22, g1Blocks: 6, g1Data: 36, g2Blocks: 2, g2Data: 37, alignment: [6, 32, 58] },
];

// GF(256) arithmetic tables with primitive polynomial 0x11d.
const GF_EXP = new Uint8Array(512);
const GF_LOG = new Uint8Array(256);
(() => {
  let x = 1;
  for (let i = 0; i < 255; i++) {
    GF_EXP[i] = x;
    GF_LOG[x] = i;
    x <<= 1;
    if (x & 0x100) x ^= 0x11d;
  }
  for (let i = 255; i < 512; i++) {
    GF_EXP[i] = GF_EXP[i - 255];
  }
})();

function gfMul(a: number, b: number): number {
  if (a === 0 || b === 0) return 0;
  return GF_EXP[GF_LOG[a] + GF_LOG[b]];
}

function rsGeneratorPoly(degree: number): number[] {
  let poly = [1];
  for (let i = 0; i < degree; i++) {
    const next = new Array<number>(poly.length + 1).fill(0);
    const root = GF_EXP[i];
    for (let j = 0; j < poly.length; j++) {
      next[j] ^= poly[j];
      next[j + 1] ^= gfMul(poly[j], root);
    }
    poly = next;
  }
  return poly;
}

function rsComputeEC(data: number[], ecLen: number): number[] {
  const gen = rsGeneratorPoly(ecLen);
  const msg = new Array<number>(data.length + ecLen).fill(0);
  for (let i = 0; i < data.length; i++) msg[i] = data[i];
  for (let i = 0; i < data.length; i++) {
    const factor = msg[i];
    if (factor === 0) continue;
    for (let j = 0; j < gen.length; j++) {
      msg[i + j] ^= gfMul(gen[j], factor);
    }
  }
  return msg.slice(data.length);
}

function bchFormatBits(ecBits: number, mask: number): number {
  const data = (ecBits << 3) | mask;
  let rem = data << 10;
  for (let i = 14; i >= 10; i--) {
    if ((rem >> i) & 1) rem ^= 0x537 << (i - 10);
  }
  return ((data << 10) | rem) ^ 0x5412;
}

function bchVersionBits(version: number): number {
  let rem = version << 12;
  for (let i = 17; i >= 12; i--) {
    if ((rem >> i) & 1) rem ^= 0x1f25 << (i - 12);
  }
  return (version << 12) | rem;
}

function buildCodewords(bytes: Uint8Array): { spec: VersionSpec; codewords: number[] } {
  let chosen: VersionSpec | undefined;
  for (const v of VERSIONS_M) {
    const ccBits = v.version < 10 ? 8 : 16;
    const totalDataBits = v.dataCodewords * 8;
    if (4 + ccBits + bytes.length * 8 <= totalDataBits) {
      chosen = v;
      break;
    }
  }
  if (!chosen) {
    throw new Error('Input too long for QR Version 12 (ECC-M)');
  }

  const bits: number[] = [];
  const pushBits = (val: number, len: number) => {
    for (let i = len - 1; i >= 0; i--) bits.push((val >> i) & 1);
  };

  pushBits(0b0100, 4); // Byte mode
  pushBits(bytes.length, chosen.version < 10 ? 8 : 16);
  for (let i = 0; i < bytes.length; i++) {
    pushBits(bytes[i], 8);
  }

  const totalDataBits = chosen.dataCodewords * 8;
  const term = Math.min(4, totalDataBits - bits.length);
  pushBits(0, term);
  while (bits.length % 8 !== 0) bits.push(0);

  const dataBytes: number[] = [];
  for (let i = 0; i < bits.length; i += 8) {
    let b = 0;
    for (let j = 0; j < 8; j++) b = (b << 1) | bits[i + j];
    dataBytes.push(b);
  }
  let padIdx = 0;
  while (dataBytes.length < chosen.dataCodewords) {
    dataBytes.push(padIdx % 2 === 0 ? 0xec : 0x11);
    padIdx++;
  }

  const dataBlocks: number[][] = [];
  const ecBlocks: number[][] = [];
  let offset = 0;
  for (let i = 0; i < chosen.g1Blocks; i++) {
    const blk = dataBytes.slice(offset, offset + chosen.g1Data);
    offset += chosen.g1Data;
    dataBlocks.push(blk);
    ecBlocks.push(rsComputeEC(blk, chosen.ecPerBlock));
  }
  for (let i = 0; i < chosen.g2Blocks; i++) {
    const blk = dataBytes.slice(offset, offset + chosen.g2Data);
    offset += chosen.g2Data;
    dataBlocks.push(blk);
    ecBlocks.push(rsComputeEC(blk, chosen.ecPerBlock));
  }

  const interleaved: number[] = [];
  const maxDataLen = Math.max(chosen.g1Data, chosen.g2Data);
  for (let col = 0; col < maxDataLen; col++) {
    for (const blk of dataBlocks) {
      if (col < blk.length) interleaved.push(blk[col]);
    }
  }
  for (let col = 0; col < chosen.ecPerBlock; col++) {
    for (const blk of ecBlocks) {
      interleaved.push(blk[col]);
    }
  }

  return { spec: chosen, codewords: interleaved };
}

function drawPatterns(size: number, spec: VersionSpec): { modules: boolean[][]; isFunc: boolean[][] } {
  const modules = Array.from({ length: size }, () => Array<boolean>(size).fill(false));
  const isFunc = Array.from({ length: size }, () => Array<boolean>(size).fill(false));

  const setFunc = (r: number, c: number, val: boolean) => {
    if (r >= 0 && r < size && c >= 0 && c < size) {
      modules[r][c] = val;
      isFunc[r][c] = true;
    }
  };

  const drawFinder = (r0: number, c0: number) => {
    for (let dr = -1; dr <= 7; dr++) {
      for (let dc = -1; dc <= 7; dc++) {
        const rr = r0 + dr;
        const cc = c0 + dc;
        if (rr < 0 || rr >= size || cc < 0 || cc >= size) continue;
        const dist = Math.max(Math.abs(dr - 3), Math.abs(dc - 3));
        setFunc(rr, cc, dist !== 2 && dist <= 3);
      }
    }
  };

  drawFinder(0, 0);
  drawFinder(0, size - 7);
  drawFinder(size - 7, 0);

  // Timing patterns
  for (let i = 8; i < size - 8; i++) {
    setFunc(6, i, i % 2 === 0);
    setFunc(i, 6, i % 2 === 0);
  }

  // Dark module
  setFunc(size - 8, 8, true);

  // Alignment patterns
  const pos = spec.alignment;
  for (let i = 0; i < pos.length; i++) {
    for (let j = 0; j < pos.length; j++) {
      if ((i === 0 && j === 0) || (i === 0 && j === pos.length - 1) || (i === pos.length - 1 && j === 0)) {
        continue;
      }
      const r0 = pos[i];
      const c0 = pos[j];
      for (let dr = -2; dr <= 2; dr++) {
        for (let dc = -2; dc <= 2; dc++) {
          const dist = Math.max(Math.abs(dr), Math.abs(dc));
          setFunc(r0 + dr, c0 + dc, dist === 0 || dist === 2);
        }
      }
    }
  }

  // Reserve format info
  for (let i = 0; i <= 8; i++) {
    if (i !== 6) {
      isFunc[8][i] = true;
      isFunc[i][8] = true;
    }
  }
  for (let i = size - 8; i < size; i++) {
    isFunc[8][i] = true;
    isFunc[i][8] = true;
  }

  // Version info for >= 7
  if (spec.version >= 7) {
    const vBits = bchVersionBits(spec.version);
    for (let i = 0; i < 18; i++) {
      const bit = ((vBits >> i) & 1) === 1;
      const a = Math.floor(i / 3);
      const b = i % 3;
      setFunc(size - 11 + b, a, bit);
      setFunc(a, size - 11 + b, bit);
    }
  }

  return { modules, isFunc };
}

function applyMaskAndFormat(modules: boolean[][], isFunc: boolean[][], size: number, mask: number) {
  const maskBit = (r: number, c: number): boolean => {
    switch (mask) {
      case 0: return (r + c) % 2 === 0;
      case 1: return r % 2 === 0;
      case 2: return c % 3 === 0;
      case 3: return (r + c) % 3 === 0;
      case 4: return (Math.floor(r / 2) + Math.floor(c / 3)) % 2 === 0;
      case 5: return ((r * c) % 2 + (r * c) % 3) === 0;
      case 6: return (((r * c) % 2 + (r * c) % 3) % 2) === 0;
      default: return (((r + c) % 2 + (r * c) % 3) % 2) === 0;
    }
  };

  for (let r = 0; r < size; r++) {
    for (let c = 0; c < size; c++) {
      if (!isFunc[r][c] && maskBit(r, c)) {
        modules[r][c] = !modules[r][c];
      }
    }
  }

  // Format bits for ECC-M (0b00)
  const fmt = bchFormatBits(0b00, mask);
  for (let i = 0; i <= 5; i++) modules[8][i] = ((fmt >> i) & 1) === 1;
  modules[8][7] = ((fmt >> 6) & 1) === 1;
  modules[8][8] = ((fmt >> 7) & 1) === 1;
  modules[7][8] = ((fmt >> 8) & 1) === 1;
  for (let i = 9; i < 15; i++) modules[14 - i][8] = ((fmt >> i) & 1) === 1;
  for (let i = 0; i < 8; i++) modules[size - 1 - i][8] = ((fmt >> i) & 1) === 1;
  for (let i = 8; i < 15; i++) modules[8][size - 15 + i] = ((fmt >> i) & 1) === 1;
}

function penaltyScore(modules: boolean[][], size: number): number {
  let score = 0;
  // Rule 1: runs of 5+ same color
  for (let r = 0; r < size; r++) {
    let runColor = modules[r][0];
    let runLen = 1;
    for (let c = 1; c < size; c++) {
      if (modules[r][c] === runColor) {
        runLen++;
      } else {
        if (runLen >= 5) score += 3 + (runLen - 5);
        runColor = modules[r][c];
        runLen = 1;
      }
    }
    if (runLen >= 5) score += 3 + (runLen - 5);
  }
  for (let c = 0; c < size; c++) {
    let runColor = modules[0][c];
    let runLen = 1;
    for (let r = 1; r < size; r++) {
      if (modules[r][c] === runColor) {
        runLen++;
      } else {
        if (runLen >= 5) score += 3 + (runLen - 5);
        runColor = modules[r][c];
        runLen = 1;
      }
    }
    if (runLen >= 5) score += 3 + (runLen - 5);
  }
  // Rule 2: 2x2 blocks
  for (let r = 0; r < size - 1; r++) {
    for (let c = 0; c < size - 1; c++) {
      const b = modules[r][c];
      if (b === modules[r][c + 1] && b === modules[r + 1][c] && b === modules[r + 1][c + 1]) {
        score += 3;
      }
    }
  }
  return score;
}

export function encodeQrMatrix(text: string): boolean[][] {
  const bytes = new TextEncoder().encode(text);
  const { spec, codewords } = buildCodewords(bytes);
  const size = spec.version * 4 + 17;
  const { modules: base, isFunc } = drawPatterns(size, spec);

  // Place data codewords in standard zig-zag order
  const bitStream: boolean[] = [];
  for (const cw of codewords) {
    for (let b = 7; b >= 0; b--) bitStream.push(((cw >> b) & 1) === 1);
  }
  let bitIdx = 0;
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5;
    for (let vert = 0; vert < size; vert++) {
      for (let j = 0; j < 2; j++) {
        const c = right - j;
        const upward = ((right + 1) & 2) === 0;
        const r = upward ? size - 1 - vert : vert;
        if (!isFunc[r][c] && bitIdx < bitStream.length) {
          base[r][c] = bitStream[bitIdx];
          bitIdx++;
        }
      }
    }
  }

  let bestMask = 0;
  let bestScore = Infinity;
  let bestMatrix = base;
  for (let mask = 0; mask < 8; mask++) {
    const trial = base.map((row) => row.slice());
    applyMaskAndFormat(trial, isFunc, size, mask);
    const s = penaltyScore(trial, size);
    if (s < bestScore) {
      bestScore = s;
      bestMask = mask;
      bestMatrix = trial;
    }
  }
  void bestMask;
  return bestMatrix;
}

export function renderQrPathD(matrix: boolean[][]): string {
  const parts: string[] = [];
  for (let r = 0; r < matrix.length; r++) {
    for (let c = 0; c < matrix[r].length; c++) {
      if (matrix[r][c]) {
        parts.push(`M${c},${r}h1v1h-1z`);
      }
    }
  }
  return parts.join('');
}
