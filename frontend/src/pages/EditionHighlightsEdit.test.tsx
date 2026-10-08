import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { EditionHighlightsEdit, MAX_HIGHLIGHTS } from './EditionHighlightsEdit';
import { pickableChapters } from '../utils/pickableChapters';
import { chaptersApi, editionsApi } from '../services/api';
import type { Chapter, EditionHighlight, VolumeSummary } from '../types';

vi.mock('../services/api', () => ({
  editionsApi: { get: vi.fn(), volumes: vi.fn(), highlights: vi.fn(), saveHighlights: vi.fn() },
  chaptersApi: { list: vi.fn() },
}));
vi.mock('react-hot-toast', () => ({ default: { success: vi.fn() } }));

const EDITION = {
  id: 1,
  title: 'К. Маркс и Ф. Энгельс. Сочинения',
  slug: 'mae-2',
  description: '',
  created_at: '',
  updated_at: '',
};
const VOLUME = {
  id: 47,
  title: 'Том 23',
  slug: 'mae-t23',
  volume_number: 23,
  author: '',
  language: '',
  country: '',
  file_path: '',
  status: 'draft',
  page_offset: 0,
  owner_id: 1,
  created_at: '',
  updated_at: '',
  pages_total: 900,
  pages_by_status: {},
  chapters_total: 2,
  role: 'volume',
} as VolumeSummary;
function chapter(id: number, title: string, over: Partial<Chapter> = {}): Chapter {
  return {
    id,
    work_id: 47,
    title,
    slug: '',
    type: 'chapter',
    order_number: id,
    start_page: 1,
    end_page: 2,
    is_apparatus: false,
    created_at: '',
    updated_at: '',
    ...over,
  };
}
const SAVED: EditionHighlight = {
  chapter_id: 9,
  chapter_slug: '',
  chapter_title: 'Немецкая идеология',
  work_id: 3,
  work_slug: 'mae-t03',
  volume_number: 3,
  volume_part: null,
  label: '',
};

function ok<T>(data: T) {
  return Promise.resolve({ data } as never);
}

function renderEditor() {
  return render(
    <MemoryRouter initialEntries={['/editions/1-mae-2/highlights']}>
      <Routes>
        <Route path="/editions/:id/highlights" element={<EditionHighlightsEdit />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(editionsApi.get).mockReturnValue(ok(EDITION));
  vi.mocked(editionsApi.volumes).mockReturnValue(ok([VOLUME]));
  vi.mocked(editionsApi.highlights).mockReturnValue(ok([SAVED]));
  vi.mocked(chaptersApi.list).mockReturnValue(
    ok([
      chapter(1, 'КАПИТАЛ. Критика политической экономии', {
        children: [chapter(2, 'Отдел первый')],
      }),
      chapter(3, 'Примечания', {
        is_apparatus: true,
        children: [chapter(4, 'К главе 1', { is_apparatus: true })],
      }),
    ]),
  );
});

describe('pickableChapters', () => {
  it('плоский список с глубиной, без аппарата и его поддерева', () => {
    const flat = pickableChapters([
      chapter(1, 'А', { children: [chapter(2, 'Б')] }),
      chapter(3, 'Примечания', { is_apparatus: true, children: [chapter(4, 'В')] }),
    ]);
    expect(flat.map((x) => [x.chapter.id, x.depth])).toEqual([
      [1, 0],
      [2, 1],
    ]);
  });
});

describe('EditionHighlightsEdit', () => {
  it('добавляет главу и сохраняет список целиком', async () => {
    const user = userEvent.setup();
    vi.mocked(editionsApi.saveHighlights).mockImplementation((_id, items) =>
      ok(
        items.map((it, i) => ({
          ...SAVED,
          chapter_id: it.chapter_id,
          label: it.label,
          work_id: i,
        })),
      ),
    );
    renderEditor();
    await screen.findByLabelText(/^Подпись/);
    await user.selectOptions(screen.getByLabelText('Том'), '47');
    await waitFor(() => expect(screen.getByLabelText('Глава')).not.toBeDisabled());
    // Аппарата в списке глав нет.
    expect(screen.queryByRole('option', { name: /Примечания/ })).toBeNull();
    await user.selectOptions(screen.getByLabelText('Глава'), '1');
    await user.click(screen.getByRole('button', { name: 'Добавить' }));
    await user.type(screen.getAllByLabelText(/^Подпись/)[1], 'Капитал, т. I');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() =>
      expect(editionsApi.saveHighlights).toHaveBeenCalledWith(1, [
        { chapter_id: 9, label: '' },
        { chapter_id: 1, label: 'Капитал, т. I' },
      ]),
    );
  });

  it('переставляет и убирает пункты', async () => {
    const user = userEvent.setup();
    vi.mocked(editionsApi.highlights).mockReturnValue(
      ok([SAVED, { ...SAVED, chapter_id: 10, chapter_title: 'Нищета философии' }]),
    );
    vi.mocked(editionsApi.saveHighlights).mockImplementation((_id, items) =>
      ok(items.map((it) => ({ ...SAVED, ...it }))),
    );
    renderEditor();
    await user.click(await screen.findByRole('button', { name: 'Выше: Нищета философии' }));
    await user.click(screen.getByRole('button', { name: 'Убрать: Немецкая идеология' }));
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() =>
      expect(editionsApi.saveHighlights).toHaveBeenCalledWith(1, [{ chapter_id: 10, label: '' }]),
    );
  });

  it('показывает текст отказа сервера', async () => {
    const user = userEvent.setup();
    vi.mocked(editionsApi.saveHighlights).mockRejectedValue({
      isAxiosError: true,
      response: { status: 400, data: { message: 'В списке есть глава не из этого собрания' } },
    });
    renderEditor();
    await user.type(await screen.findByLabelText(/^Подпись/), 'x');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }));
    expect(await screen.findByText('В списке есть глава не из этого собрания')).toBeInTheDocument();
  });

  it('без изменений сохранять нечего, при восьми пунктах добавлять некуда', async () => {
    vi.mocked(editionsApi.highlights).mockReturnValue(
      ok(Array.from({ length: MAX_HIGHLIGHTS }, (_, i) => ({ ...SAVED, chapter_id: 100 + i }))),
    );
    renderEditor();
    expect(await screen.findByRole('button', { name: 'Сохранить' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Добавить' })).toBeDisabled();
    expect(screen.getByText(/не больше 8/)).toBeInTheDocument();
  });

  it('ответ прежнего тома не перезаписывает главы нового', async () => {
    const user = userEvent.setup();
    const volumeB = { ...VOLUME, id: 48, title: 'Том 24', volume_number: 24 } as VolumeSummary;
    vi.mocked(editionsApi.volumes).mockReturnValue(ok([VOLUME, volumeB]));
    let resolveA: (v: never) => void = () => {};
    vi.mocked(chaptersApi.list).mockImplementation((workId) =>
      workId === 47
        ? new Promise((r) => {
            resolveA = r;
          })
        : ok([chapter(20, 'Глава из тома B')]),
    );
    renderEditor();
    await screen.findByLabelText(/^Подпись/);
    await user.selectOptions(screen.getByLabelText('Том'), '47');
    await user.selectOptions(screen.getByLabelText('Том'), '48');
    await screen.findByRole('option', { name: /Глава из тома B/ });
    resolveA({ data: [chapter(10, 'Глава из тома A')] } as never);
    await waitFor(() =>
      expect(screen.queryByRole('option', { name: /Глава из тома A/ })).toBeNull(),
    );
    expect(screen.getByRole('option', { name: /Глава из тома B/ })).toBeInTheDocument();
  });

  it('при неудачной загрузке редактора нет, есть текст ошибки', async () => {
    vi.mocked(editionsApi.highlights).mockRejectedValue({
      isAxiosError: true,
      response: { status: 500, data: { message: 'База недоступна' } },
    });
    renderEditor();
    expect(await screen.findByText('База недоступна')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Сохранить' })).toBeNull();
  });
});
