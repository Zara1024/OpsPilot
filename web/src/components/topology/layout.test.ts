import { expect, it } from 'vitest';
import { alignResourceColumns } from './layout';

it('keeps service/device/cluster columns with missing services and prevents snapped overlaps', () => {
  const positions = alignResourceColumns([
    { id: 'service', type: 'service', x: 40, y: 164 },
    { id: 'device', type: 'device', x: 310, y: 164 },
    { id: 'cluster', type: 'cluster', x: 580, y: 164 },
    { id: 'device-only', type: 'device', x: 40, y: 40 },
    { id: 'cluster-only', type: 'cluster', x: 310, y: 40 },
    { id: 'same-type', type: 'device', x: 580, y: 164 },
  ], 160, 44);
  expect(positions.get('service')).toEqual({ x: 40, y: 164 });
  expect(positions.get('device')).toEqual({ x: 310, y: 164 });
  expect(positions.get('cluster')).toEqual({ x: 580, y: 164 });
  expect(positions.get('device-only')).toEqual({ x: 310, y: 40 });
  expect(positions.get('cluster-only')).toEqual({ x: 580, y: 40 });
  expect(positions.get('same-type')).toEqual({ x: 310, y: 288 });
});

it('preserves multi-tier microservice architecture ranks across distinct service columns', () => {
  const positions = alignResourceColumns([
    { id: 'gateway', type: 'service', x: 40, y: 100 },
    { id: 'frontend', type: 'service', x: 310, y: 100 },
    { id: 'checkout', type: 'service', x: 580, y: 100 },
    { id: 'payment', type: 'service', x: 850, y: 100 },
    { id: 'host', type: 'device', x: 1120, y: 100 },
    { id: 'k8s', type: 'cluster', x: 1390, y: 100 },
  ], 160, 44);

  // Each tier gets its own column: 40 + col * 270
  expect(positions.get('gateway')).toEqual({ x: 40, y: 100 });
  expect(positions.get('frontend')).toEqual({ x: 310, y: 100 });
  expect(positions.get('checkout')).toEqual({ x: 580, y: 100 });
  expect(positions.get('payment')).toEqual({ x: 850, y: 100 });
  expect(positions.get('host')).toEqual({ x: 1120, y: 100 });
  expect(positions.get('k8s')).toEqual({ x: 1390, y: 100 });
});
