import { useMemo } from 'react';
import { encodeQrMatrix, renderQrPathD } from '@/lib/qrcode';

type Props = {
  value: string;
  size?: number;
  label?: string;
};

export function QrCode({ value, size = 176, label }: Props) {
  const { viewBox, pathD } = useMemo(() => {
    try {
      const matrix = encodeQrMatrix(value);
      const n = matrix.length;
      return {
        viewBox: `-4 -4 ${n + 8} ${n + 8}`,
        pathD: renderQrPathD(matrix),
      };
    } catch {
      return { viewBox: '0 0 1 1', pathD: '' };
    }
  }, [value]);

  if (!pathD) return null;

  return (
    <div className="inline-flex rounded-xl border border-border bg-white p-2 shadow-sm">
      <svg
        width={size}
        height={size}
        viewBox={viewBox}
        role="img"
        aria-label={label ?? 'QR Code'}
        shapeRendering="crispEdges"
      >
        <rect x="-4" y="-4" width="100%" height="100%" fill="#ffffff" />
        <path d={pathD} fill="#09090b" />
      </svg>
    </div>
  );
}
