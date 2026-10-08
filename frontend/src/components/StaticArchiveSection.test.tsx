import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import { StaticArchiveSection } from './StaticArchiveSection';
import { staticArchiveApi } from '../services/api';
import type { StaticArchive } from '../types';

type Answer = Awaited<ReturnType<typeof staticArchiveApi.get>>;

const ARCHIVE: StaticArchive = {
  file: 'chitalnya-2026-10-06.zip',
  url: 'https://s3.example.org/chitalnya/chitalnya-2026-10-06.zip',
  size: 418756548,
  sha256: 'f987dddc719662494949b10a4e8de3809c94e7097c733dc8f7f0772e81469416',
  date: '2026-10-06',
  works: 203,
};

afterEach(() => vi.restoreAllMocks());

describe('раздел «Вся читальня одним архивом»', () => {
  it('даёт кнопку, дату, размер, сумму и прямой адрес', async () => {
    vi.spyOn(staticArchiveApi, 'get').mockImplementation(() =>
      Promise.resolve({ data: ARCHIVE } as Answer),
    );
    render(<StaticArchiveSection />);
    const link = await screen.findByRole('link', { name: 'Скачать архив — 399 МБ' });
    expect(link).toHaveAttribute('href', '/api/static-archive/download');
    expect(
      screen.getByText('ZIP, собран 6 октября 2026 г.; томов и работ — 203'),
    ).toBeInTheDocument();
    expect(screen.getByText(ARCHIVE.sha256)).toBeInTheDocument();
    expect(screen.getByText(ARCHIVE.url)).toBeInTheDocument();
    expect(screen.queryByText(/пересобирается — загляните позже/)).toBeNull();
  });

  it('без архива говорит, что он пересобирается, и кнопки не даёт', async () => {
    vi.spyOn(staticArchiveApi, 'get').mockImplementation(() => Promise.reject(new Error('404')));
    render(<StaticArchiveSection />);
    expect(
      await screen.findByText('Архив сейчас пересобирается — загляните позже.'),
    ).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /Скачать архив/ })).toBeNull();
    expect(screen.getByText('Как открыть.')).toBeInTheDocument();
  });

  // Пока сведения едут, раздел не обещает ни кнопки, ни пересборки — и якорь
  // для ссылки из подвала уже на месте.
  it('пока сведения едут, стоит заголовок с якорем и ничего лишнего', async () => {
    let answer: (v: Answer) => void = () => {};
    vi.spyOn(staticArchiveApi, 'get').mockImplementation(
      () => new Promise<Answer>((resolve) => (answer = resolve)),
    );
    render(<StaticArchiveSection />);
    expect(document.getElementById('offline')).not.toBeNull();
    expect(screen.queryByRole('link', { name: /Скачать архив/ })).toBeNull();
    expect(screen.queryByText(/пересобирается — загляните позже/)).toBeNull();
    await act(async () => answer({ data: ARCHIVE } as Answer));
    expect(screen.getByRole('link', { name: /Скачать архив/ })).toBeInTheDocument();
  });
});
