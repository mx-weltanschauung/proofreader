import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { closuresApi } from '../../services/api';
import type { ClosureEntry } from '../../types';
import { ClosuresAdmin } from './ClosuresAdmin';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const ROWS: ClosureEntry[] = [
  {
    kind: 'work',
    entity_id: 7,
    title: 'Том 7',
    closed: true,
    reason: 'письмо издателя о правах',
    actor_name: 'admin',
    at: '2026-10-23T10:00:00Z',
  },
  {
    kind: 'chapter',
    entity_id: 9,
    title: 'Глава девятая',
    closed: false,
    reason: 'закрыли по ошибке',
    actor_name: 'admin',
    at: '2026-10-22T10:00:00Z',
  },
];

function renderPage() {
  return render(
    <MemoryRouter>
      <ClosuresAdmin />
    </MemoryRouter>,
  );
}

describe('ClosuresAdmin', () => {
  beforeEach(() => vi.restoreAllMocks());
  afterEach(() => vi.restoreAllMocks());

  it('рисует строку журнала с причиной, видом, действием и автором', async () => {
    vi.spyOn(closuresApi, 'list').mockReturnValue(ok(ROWS));
    renderPage();
    await waitFor(() => expect(screen.getByText('письмо издателя о правах')).toBeInTheDocument());
    expect(screen.getByText('23.10.2026')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Том 7' })).toHaveAttribute('href', '/works/7');
    expect(screen.getByText('Глава девятая')).toBeInTheDocument();
    expect(screen.getByText('закрыто')).toBeInTheDocument();
    expect(screen.getByText('открыто')).toBeInTheDocument();
    expect(screen.getAllByText('admin')).toHaveLength(2);
  });

  it('пустой журнал говорит об этом', async () => {
    vi.spyOn(closuresApi, 'list').mockReturnValue(ok([]));
    renderPage();
    await waitFor(() => expect(screen.getByText('Закрытий пока не было.')).toBeInTheDocument());
  });

  it('показывает текст ошибки сервера', async () => {
    vi.spyOn(closuresApi, 'list').mockRejectedValue({
      response: { status: 403, data: { message: 'Только администратор' } },
    });
    renderPage();
    await waitFor(() => expect(screen.getByText('Только администратор')).toBeInTheDocument());
  });
});
