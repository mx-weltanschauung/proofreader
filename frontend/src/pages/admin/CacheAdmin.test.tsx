import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { cacheApi } from '../../services/api';
import type { CacheStats } from '../../types';
import { CacheAdmin } from './CacheAdmin';

vi.mock('../../services/api', () => ({
  cacheApi: { stats: vi.fn(), purgeAll: vi.fn() },
}));

vi.mock('react-hot-toast', () => ({
  default: { success: vi.fn(), error: vi.fn() },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const STATS: CacheStats = {
  enabled: true,
  files: 128,
  bytes: 41 * 1024 * 1024,
  oldest_age_seconds: 1800,
};

beforeEach(() => {
  vi.clearAllMocks();
});

describe('CacheAdmin', () => {
  it('показывает сводку', async () => {
    vi.mocked(cacheApi.stats).mockReturnValue(ok(STATS));

    render(
      <MemoryRouter>
        <CacheAdmin />
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.getByText('128')).toBeInTheDocument());
    expect(screen.getByText(/41(,|\.)0 МБ/)).toBeInTheDocument();
  });

  // Пустой PAGE_CACHE_DIR на боевом иначе ничем себя не выдаст.
  it('прямо говорит, что кэш выключен', async () => {
    vi.mocked(cacheApi.stats).mockReturnValue(
      ok({ enabled: false, files: 0, bytes: 0, oldest_age_seconds: 0 }),
    );

    render(
      <MemoryRouter>
        <CacheAdmin />
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.getByText(/выключен/i)).toBeInTheDocument());
  });

  it('сбрасывает кэш и перечитывает сводку', async () => {
    vi.mocked(cacheApi.stats).mockReturnValue(ok(STATS));
    vi.mocked(cacheApi.purgeAll).mockReturnValue(ok({ removed: 128, bytes: STATS.bytes }));

    render(
      <MemoryRouter>
        <CacheAdmin />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText('128')).toBeInTheDocument());

    await userEvent.click(screen.getByRole('button', { name: /Сбросить весь кэш/i }));
    await userEvent.click(screen.getByRole('button', { name: /Да, сбросить/i }));

    await waitFor(() => expect(cacheApi.purgeAll).toHaveBeenCalled());
    await waitFor(() => expect(vi.mocked(cacheApi.stats).mock.calls.length).toBeGreaterThan(1));
  });
});
