import { describe, expect, it } from 'vitest';
import { encodeQrMatrix, renderQrPathD } from './qrcode';

describe('qrcode generator', () => {
  it('encodes otpauth URIs into valid square QR matrices with finder patterns', () => {
    const uri = 'otpauth://totp/OpsPilot:alice@corp.example.com?algorithm=SHA1&digits=6&issuer=OpsPilot&period=30&secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP';
    const matrix = encodeQrMatrix(uri);
    const n = matrix.length;
    expect(n).toBeGreaterThanOrEqual(41); // >= version 6 for ~131 bytes at ECC-M
    expect(matrix.every((row) => row.length === n)).toBe(true);

    // Top-left 7x7 finder outer border must be dark, center 3x3 dark, ring around center light
    expect(matrix[0][0]).toBe(true);
    expect(matrix[0][6]).toBe(true);
    expect(matrix[6][0]).toBe(true);
    expect(matrix[1][1]).toBe(false);
    expect(matrix[3][3]).toBe(true);

    // Top-right and bottom-left finders
    expect(matrix[0][n - 1]).toBe(true);
    expect(matrix[n - 1][0]).toBe(true);

    const d = renderQrPathD(matrix);
    expect(d.length).toBeGreaterThan(200);
    expect(d.startsWith('M')).toBe(true);
  });

  it('throws on oversized input beyond Version 12', () => {
    expect(() => encodeQrMatrix('x'.repeat(500))).toThrow();
  });
});
