type LayoutNode = { id: string; type: string; x: number; y: number };

// Keep the resource columns stable even when an upstream node is absent.
// For multi-tier microservice architectures, preserve distinct Dagre ranks per service tier.
// Dagre still chooses row order; only resolve collisions introduced by snapping.
export function alignResourceColumns(nodes: LayoutNode[], width: number, height: number) {
  const serviceXs = [...new Set(nodes.filter((n) => n.type === 'service').map((n) => n.x))].sort((a, b) => a - b);

  // If there's at most 1 service rank, preserve legacy behavior (service: 0, device: 1, cluster: 2)
  if (serviceXs.length <= 1) {
    const columns: Record<string, number> = { service: 0, device: 1, network_device: 1, cluster: 2 };
    const otherXs = [...new Set(nodes.filter((n) => columns[n.type] === undefined).map((n) => n.x))].sort((a, b) => a - b);
    const bottom = new Map<number, number>();
    const positions = new Map<string, { x: number; y: number }>();
    for (const node of [...nodes].sort((a, b) => a.y - b.y || a.x - b.x || a.id.localeCompare(b.id))) {
      const column = columns[node.type] ?? 3 + otherXs.indexOf(node.x);
      const x = 40 + column * (width + 110);
      const y = Math.max(node.y, bottom.get(column) ?? 40);
      positions.set(node.id, { x, y });
      bottom.set(column, y + height + 80);
    }
    return positions;
  }

  // Multi-tier microservice architecture:
  // Map each service Dagre rank to its own column, then devices and clusters follow.
  const numServiceCols = serviceXs.length;
  const devXs = [...new Set(nodes.filter((n) => n.type === 'device' || n.type === 'network_device').map((n) => n.x))].sort((a, b) => a - b);
  const clusterXs = [...new Set(nodes.filter((n) => n.type === 'cluster').map((n) => n.x))].sort((a, b) => a - b);
  const otherXs = [...new Set(nodes.filter((n) => !['service', 'device', 'network_device', 'cluster'].includes(n.type)).map((n) => n.x))].sort((a, b) => a - b);

  const getCol = (node: LayoutNode) => {
    if (node.type === 'service') {
      return serviceXs.indexOf(node.x);
    }
    if (node.type === 'device' || node.type === 'network_device') {
      return numServiceCols + devXs.indexOf(node.x);
    }
    if (node.type === 'cluster') {
      return numServiceCols + devXs.length + clusterXs.indexOf(node.x);
    }
    return numServiceCols + devXs.length + clusterXs.length + otherXs.indexOf(node.x);
  };

  const bottom = new Map<number, number>();
  const positions = new Map<string, { x: number; y: number }>();
  for (const node of [...nodes].sort((a, b) => a.y - b.y || a.x - b.x || a.id.localeCompare(b.id))) {
    const col = getCol(node);
    const x = 40 + col * (width + 110);
    const y = Math.max(node.y, bottom.get(col) ?? 40);
    positions.set(node.id, { x, y });
    bottom.set(col, y + height + 80);
  }
  return positions;
}
