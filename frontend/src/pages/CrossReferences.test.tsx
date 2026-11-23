import { afterEach, describe, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { crossReferencesApi, editionsApi, readingApi } from '../services/api';
import type { CrossReferenceCandidate, Edition, VolumeSummary } from '../types';
import { CrossReferences } from './CrossReferences';

const edition = {
  id: 12,
  title: 'Сборник, 2-е издание',
  slug: 'example',
  cross_references_enabled: true,
} as Edition;
const volume = { id: 3, title: 'Первый том', volume_number: 1, role: 'volume' } as VolumeSummary;
const otherVolume = {
  id: 8,
  title: 'Второй том',
  volume_number: 2,
  role: 'volume',
} as VolumeSummary;
const unnumbered = { id: 14, title: 'Указатель', role: 'volume' } as VolumeSummary;
const candidate: CrossReferenceCandidate = {
  id: 41,
  quote: 'см. т. 2, с. 47',
  context: 'Ранее об этом уже говорилось (см. т. 2, с. 47).',
  status: 'pending',
  issue: 'ambiguous',
  volume: 2,
  printed_page: 47,
  source: { work_id: 3, page_id: 74, page_number: 10, printed_page: 5, work_title: 'Первый том' },
  targets: [
    {
      work_id: 8,
      page_id: 404,
      page_number: 55,
      printed_page: 47,
      work_title: 'Второй том',
      volume_part: 'I',
    },
    {
      work_id: 9,
      page_id: 405,
      page_number: 60,
      printed_page: 47,
      work_title: 'Продолжение второго тома',
      volume_part: 'II',
      closed: true,
    },
  ],
};

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as AxiosResponse<T>);
}

function setup(currentEdition = edition) {
  vi.spyOn(editionsApi, 'get').mockReturnValue(ok(currentEdition));
  vi.spyOn(editionsApi, 'volumes').mockReturnValue(ok([unnumbered, volume, otherVolume]));
  const renderPage = vi.spyOn(readingApi, 'window').mockImplementation((workId, pageNumber) =>
    ok({
      pages: [
        {
          page_number: pageNumber,
          html:
            (workId === 3 ? '<p>Текст источника</p>' : '<p>Текст цели</p>') +
            `<sup id="fnref:${pageNumber}-r2"><a href="#fn:${pageNumber}-r2">К сноске</a></sup>`,
          notes_html: `<p id="fn:${pageNumber}-r2">${workId === 3 ? 'Печатная сноска с отсылкой' : 'Сноска цели'}</p>`,
          blank: false,
        },
      ],
      next_from: null,
      total_pages: 60,
    }),
  );
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/editions/12-example/cross-references']}>
        <Routes>
          <Route path="/editions/:id/cross-references" element={<CrossReferences />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { user, renderPage };
}

afterEach(() => vi.restoreAllMocks());

describe('Сверка отсылок', () => {
  it('не загружает очередь для собрания без разрешённой сверки', async () => {
    const list = vi.spyOn(crossReferencesApi, 'list').mockReturnValue(ok({ items: [] }));
    setup({ ...edition, cross_references_enabled: undefined });

    expect(
      await screen.findByText('Сверка отсылок для этого собрания недоступна.'),
    ).toBeInTheDocument();
    expect(editionsApi.volumes).not.toHaveBeenCalled();
    expect(list).not.toHaveBeenCalled();
    expect(screen.queryByRole('button', { name: 'Найти отсылки' })).not.toBeInTheDocument();
  });

  it('показывает неоднозначность и подтверждает выбранную полосу после сравнения', async () => {
    let confirmed = false;
    vi.spyOn(crossReferencesApi, 'list').mockImplementation((_edition, _work, status) =>
      ok({
        items:
          confirmed && status === 'confirmed'
            ? [{ ...candidate, status: 'confirmed', target_page_id: 405 }]
            : confirmed
              ? []
              : [candidate],
      }),
    );
    const decide = vi.spyOn(crossReferencesApi, 'decide').mockImplementation(() => {
      confirmed = true;
      return ok(undefined);
    });
    const { user, renderPage } = setup();

    expect(await screen.findByText('Текст источника')).toBeInTheDocument();
    expect(screen.getByText('Печатная сноска с отсылкой')).toBeInTheDocument();
    expect(screen.getByText('Найдено несколько возможных полос.')).toBeInTheDocument();
    expect(
      screen.getByText('Номер издания в отсылке не указан. Сверьте источник по контексту.'),
    ).toBeInTheDocument();
    const approve = screen.getByRole('button', { name: 'Подтвердить' });
    expect(approve).toBeDisabled();
    expect(renderPage).toHaveBeenCalledTimes(1);
    expect(renderPage).toHaveBeenCalledWith(3, 10, 1);

    await user.selectOptions(screen.getByLabelText('Возможная цель'), '405');
    const target = screen.getByRole('region', { name: 'Цель' });
    expect(await within(target).findByText('Текст цели')).toBeInTheDocument();
    expect(within(target).getByText(edition.title)).toBeInTheDocument();
    expect(within(target).getByText('Печатная страница 47 · полоса 60')).toBeInTheDocument();
    expect(within(target).getByText('закрыт')).toBeInTheDocument();
    expect(within(target).getByRole('link', { name: 'Открыть полосу' })).toHaveAttribute(
      'href',
      '/works/9/pages/60',
    );
    expect(renderPage).toHaveBeenCalledWith(9, 60, 1);

    await user.click(approve);
    await screen.findByText('Нет отсылок с этим статусом.');
    expect(decide).toHaveBeenCalledWith(12, 3, 41, { decision: 'confirmed', target_page_id: 405 });

    await user.selectOptions(screen.getByLabelText('Статус'), 'confirmed');
    expect(await screen.findByText('Текст цели')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Подтвердить' })).not.toBeInTheDocument();
  });

  it('не показывает соседнюю полосу, если нужная отсутствует в окне чтения', async () => {
    vi.spyOn(crossReferencesApi, 'list').mockReturnValue(ok({ items: [candidate] }));
    const { user, renderPage } = setup();
    await screen.findByText('Текст источника');
    renderPage.mockReturnValueOnce(
      ok({
        pages: [
          {
            page_number: 61,
            html: '<p>Соседняя полоса</p>',
            notes_html: '',
            blank: false,
          },
        ],
        next_from: null,
        total_pages: 61,
      }),
    );

    await user.selectOptions(screen.getByLabelText('Возможная цель'), '405');

    const target = screen.getByRole('region', { name: 'Цель' });
    expect(await within(target).findByRole('alert')).toHaveTextContent(
      'Не удалось загрузить текст полосы.',
    );
    expect(within(target).queryByText('Соседняя полоса')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Подтвердить' })).toBeDisabled();
  });

  it('сохраняет переход к сноске внутри каждой полосы с одинаковым номером', async () => {
    vi.spyOn(crossReferencesApi, 'list').mockReturnValue(
      ok({ items: [{ ...candidate, targets: [{ ...candidate.targets[0], page_number: 10 }] }] }),
    );
    const { user } = setup();
    await screen.findByText('Текст источника');
    await user.selectOptions(screen.getByLabelText('Возможная цель'), '404');

    const source = within(screen.getByRole('region', { name: 'Источник' }));
    const target = within(screen.getByRole('region', { name: 'Цель' }));
    const sourceNote = source.getByText('Печатная сноска с отсылкой');
    const targetNote = await target.findByText('Сноска цели');
    expect(sourceNote.id).not.toBe(targetNote.id);
    expect(source.getByRole('link', { name: 'К сноске' })).toHaveAttribute(
      'href',
      `#${sourceNote.id}`,
    );
    expect(target.getByRole('link', { name: 'К сноске' })).toHaveAttribute(
      'href',
      `#${targetNote.id}`,
    );
  });

  it('запускает поиск для выбранного тома, догружает очередь и обновляет её после конфликта', async () => {
    const laterCandidate: CrossReferenceCandidate = {
      ...candidate,
      id: 42,
      quote: 'см. т. 3, с. 84',
      source: { ...candidate.source, work_id: 8 },
      targets: [],
      issue: 'missing',
    };
    let stale = false;
    const list = vi
      .spyOn(crossReferencesApi, 'list')
      .mockImplementation((_edition, work, _status, after) =>
        ok(
          work !== 8 || stale
            ? { items: [] }
            : after
              ? { items: [laterCandidate] }
              : { items: [], next_after: 41 },
        ),
      );
    const scan = vi
      .spyOn(crossReferencesApi, 'scan')
      .mockReturnValue(ok({ detected: 2, added: 0 }));
    const decide = vi.spyOn(crossReferencesApi, 'decide').mockImplementation(() => {
      stale = true;
      return Promise.reject({ response: { status: 409 } });
    });
    const { user } = setup();
    await user.selectOptions(await screen.findByLabelText('Исходный том'), '8');
    await user.click(screen.getByRole('button', { name: 'Найти отсылки' }));
    await screen.findByText('Найдено 2, добавлено 0.');
    expect(scan).toHaveBeenCalledWith(12, 8);

    await user.click(await screen.findByRole('button', { name: 'Показать ещё' }));
    await screen.findByText('Печатная страница отсутствует в этом собрании.');
    expect(list).toHaveBeenCalledWith(12, 8, 'pending', 41);
    expect(screen.getByRole('button', { name: 'Подтвердить' })).toBeDisabled();

    await user.click(screen.getByRole('button', { name: 'Отклонить' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Решение не сохранено. Отсылка изменилась или уже рассмотрена.',
    );
    expect(decide).toHaveBeenCalledWith(12, 8, 42, { decision: 'rejected' });
    await waitFor(() => expect(screen.queryByText(laterCandidate.quote)).not.toBeInTheDocument());
  });
});
