import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { journalsApi } from '../services/api';
import { JournalDetail } from './JournalDetail';
import type { JournalDetail as Detail } from '../types';

function ok<T>(data: T) {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const DETAIL: Detail = {
  journal: {
    id: 1,
    slug: 'pzm',
    title: 'Под знаменем марксизма',
    subtitle: 'философский журнал',
    description: '',
    created_at: '',
    updated_at: '',
  },
  years: [
    {
      year: 1925,
      issues: [
        {
          id: 10,
          label: '1—2',
          months: 'январь—февраль',
          work_id: 300,
          work_slug: 'pod-znamenem-marksizma-1925-1-2',
        },
        { id: 11, label: '3', months: '', work_id: 301, work_slug: '' },
      ],
    },
  ],
};

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/journals/:slug" element={<JournalDetail />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('JournalDetail', () => {
  afterEach(() => vi.restoreAllMocks());

  it('строка на год, клетка номера ведёт на карточку номера', async () => {
    vi.spyOn(journalsApi, 'get').mockImplementation(() => ok(DETAIL));
    renderAt('/journals/pzm');
    expect(
      await screen.findByRole('heading', { level: 1, name: 'Под знаменем марксизма' }),
    ).toBeInTheDocument();
    expect(screen.getByText('1925')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /№ 1—2/ })).toHaveAttribute(
      'href',
      '/works/300-pod-znamenem-marksizma-1925-1-2',
    );
    expect(screen.getByRole('link', { name: /№ 3/ })).toHaveAttribute('href', '/works/301');
    expect(screen.getByText('январь—февраль')).toBeInTheDocument();
  });

  it('неизвестный журнал — сообщение и ссылка в читальню', async () => {
    vi.spyOn(journalsApi, 'get').mockImplementation(() => Promise.reject(new Error('404')));
    renderAt('/journals/net');
    expect(await screen.findByText(/Журнал не найден|Не удалось/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /В читальню/ })).toHaveAttribute('href', '/');
  });
});
