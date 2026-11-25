import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { listIncidents } from '@/api/alerts';
import { useAuth } from './auth';
import { useIncidentBadge } from './incidentBadge';

vi.mock('@/api/alerts', () => ({
  listIncidents: vi.fn(),
}));

const originalVisibilityDescriptor =
  Object.getOwnPropertyDescriptor(document, 'visibilityState');

function setVisibilityState(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    get: () => state,
  });
}

describe('useIncidentBadge visibility-aware polling', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.resetAllMocks();
    setVisibilityState('visible');
    useAuth.getState().setSession({
      access_token: 'test-token',
      role: 'admin',
      email: 'alice@example.com',
    });
    vi.mocked(listIncidents).mockResolvedValue({ items: [], total: 2 });
  });

  afterEach(() => {
    useIncidentBadge.getState().stop();
    useIncidentBadge.setState({ openCount: 0 });
    useAuth.getState().logout();
    if (originalVisibilityDescriptor) {
      Object.defineProperty(document, 'visibilityState', originalVisibilityDescriptor);
    } else {
      delete (document as unknown as Record<string, unknown>).visibilityState;
    }
    vi.useRealTimers();
  });

  it('fetches immediately on start and polls every 30s while visible', async () => {
    useIncidentBadge.getState().start();
    await vi.advanceTimersByTimeAsync(0);
    expect(listIncidents).toHaveBeenCalledTimes(1);
    expect(useIncidentBadge.getState().openCount).toBe(2);

    await vi.advanceTimersByTimeAsync(30_000);
    expect(listIncidents).toHaveBeenCalledTimes(2);
  });

  it('skips interval ticks while hidden and catches up immediately when visible again', async () => {
    useIncidentBadge.getState().start();
    await vi.advanceTimersByTimeAsync(0);
    expect(listIncidents).toHaveBeenCalledTimes(1);

    setVisibilityState('hidden');
    await vi.advanceTimersByTimeAsync(90_000);
    expect(listIncidents).toHaveBeenCalledTimes(1);

    vi.mocked(listIncidents).mockResolvedValueOnce({ items: [], total: 5 });
    setVisibilityState('visible');
    document.dispatchEvent(new Event('visibilitychange'));
    await vi.advanceTimersByTimeAsync(0);

    expect(listIncidents).toHaveBeenCalledTimes(2);
    expect(useIncidentBadge.getState().openCount).toBe(5);
  });

  it('cleans up both interval timer and visibilitychange listener on stop()', async () => {
    useIncidentBadge.getState().start();
    await vi.advanceTimersByTimeAsync(0);
    expect(listIncidents).toHaveBeenCalledTimes(1);

    useIncidentBadge.getState().stop();
    await vi.advanceTimersByTimeAsync(60_000);
    setVisibilityState('hidden');
    setVisibilityState('visible');
    document.dispatchEvent(new Event('visibilitychange'));
    await vi.advanceTimersByTimeAsync(0);

    expect(listIncidents).toHaveBeenCalledTimes(1);
  });
});
