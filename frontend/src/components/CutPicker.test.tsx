import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { CutPicker } from './CutPicker';
import {
  shelfApi,
  worksApi,
  chaptersApi,
  pagesApi,
  searchApi,
  documentsApi,
} from '../services/api';
import type { Work, Page, Shelf, PageBlock } from '../types';

vi.mock('../services/api', () => ({
  shelfApi: { get: vi.fn() },
  worksApi: { get: vi.fn(), pageMap: vi.fn() },
  chaptersApi: { list: vi.fn() },
  pagesApi: { getByNumber: vi.fn(), blocks: vi.fn() },
  searchApi: { search: vi.fn() },
  documentsApi: { createCut: vi.fn() },
}));

const mockedShelfGet = vi.mocked(shelfApi.get);
const mockedWorkGet = vi.mocked(worksApi.get);
const mockedPageMap = vi.mocked(worksApi.pageMap);
const mockedChaptersList = vi.mocked(chaptersApi.list);
const mockedPageByNumber = vi.mocked(pagesApi.getByNumber);
const mockedBlocks = vi.mocked(pagesApi.blocks);
const mockedSearch = vi.mocked(searchApi.search);
const mockedCreateCut = vi.mocked(documentsApi.createCut);

function ok<T>(data: T): Promise<{ data: T }> {
  return Promise.resolve({ data });
}

// Издание оканчивается точкой намеренно («…, 2-е изд.») — так же, как у
// настоящего корпуса; именно этот случай и проверяет joinSourceParts на
// двойную точку в citationSignature.
const WORK: Work = {
  id: 5,
  title: 'Т. 6',
  author: '',
  edition_title: 'Сочинения, 2-е изд.',
  language: 'ru',
  country: 'СССР',
  file_path: '',
  status: 'completed',
  owner_id: 1,
  edition_id: 1,
  volume_number: 6,
  page_offset: 0,
  created_at: '',
  updated_at: '',
};

const PAGE: Page = {
  id: 900,
  work_id: 5,
  page_number: 12,
  preview_path: '',
  content_markdown: 'Ленин писал так.',
  status: 'вычитана',
  created_at: '',
  updated_at: '',
};

const PAGE2: Page = {
  ...PAGE,
  id: 901,
  page_number: 13,
};

// Разрез полосы — байтовые границы плюс готовая вёрстка, ровно то, что
// возвращает GET /works/{id}/pages/{id}/blocks (задача 9). Начало «Ленин»
// — 10 байт кириллицы, не 5 символов: подборщик передаёт эти границы как
// есть, не пересчитывая их сам.
const BLOCKS_PAGE: PageBlock[] = [
  { start: 0, end: 10, kind: 'paragraph', html: '<p>Ленин</p>' },
  { start: 12, end: 17, kind: 'paragraph', html: '<p>писал так.</p>' },
];

const BLOCKS_PAGE2: PageBlock[] = [
  { start: 0, end: 8, kind: 'paragraph', html: '<p>Продолжение.</p>' },
];

const WORK2: Work = { ...WORK, id: 6, title: 'Т. 7' };

// Ключ разбора, которым сейчас правит DocumentForm — сотруднический (без
// ника), CutPicker сам его не строит, только передаёт как есть.
const DOCUMENT_KEY = { slug: 'razbor-o-gosudarstve' };

const SHELF: Shelf = {
  editions: [
    {
      edition: {
        id: 1,
        title: 'Сочинения',
        slug: 'sochineniya',
        description: '',
        created_at: '',
        updated_at: '',
      },
      volumes: [
        {
          ...WORK,
          pages_total: 500,
          pages_by_status: {},
          chapters_total: 3,
        },
      ],
    },
  ],
  // Второй, отдельный от тома 5 том — для проверки, что переключение между
  // томами сбрасывает уже отмеченные края (в отличие от смены полосы внутри
  // одного тома, которая обязана их сохранять).
  loose_works: [{ id: 6, title: 'Т. 7' }],
};

beforeEach(() => {
  mockedShelfGet.mockReset().mockResolvedValue(ok(SHELF) as never);
  mockedWorkGet
    .mockReset()
    .mockImplementation(((id: number) => ok(id === 6 ? WORK2 : WORK)) as never);
  mockedPageMap.mockReset().mockResolvedValue(ok([]) as never);
  mockedChaptersList.mockReset().mockResolvedValue(ok([]) as never);
  mockedPageByNumber.mockReset().mockResolvedValue(ok(PAGE) as never);
  mockedBlocks
    .mockReset()
    .mockImplementation(((_workId: number, pageId: number) =>
      ok({ blocks: pageId === PAGE2.id ? BLOCKS_PAGE2 : BLOCKS_PAGE })) as never);
  mockedSearch
    .mockReset()
    .mockResolvedValue(
      ok({ query: '', terms: [], chapters: [], concepts: [], volumes: [], total_hits: 0 }) as never,
    );
  mockedCreateCut.mockReset();
});

interface RenderOpts {
  createCut?: ReturnType<typeof vi.fn>;
  onInsert?: ReturnType<typeof vi.fn>;
}

/**
 * Доводит подборщик до состояния «полоса на экране, блоки загружены»:
 * выбирает том 5 на полке, вводит номер страницы и жмёт «Загрузить».
 */
async function renderPicker({ createCut, onInsert = vi.fn() }: RenderOpts = {}) {
  if (createCut) mockedCreateCut.mockImplementation(createCut as never);

  render(<CutPicker documentKey={DOCUMENT_KEY} onInsert={onInsert} onClose={vi.fn()} />);

  await waitFor(() => expect(screen.getByLabelText('Том')).toBeInTheDocument());
  await userEvent.selectOptions(screen.getByLabelText('Том'), ['5']);
  await waitFor(() => expect(mockedWorkGet).toHaveBeenCalledWith(5));

  await waitFor(() => expect(screen.getByLabelText(/Номер страницы/)).toBeInTheDocument());
  await userEvent.type(screen.getByLabelText(/Номер страницы/), '12');
  await userEvent.click(screen.getByRole('button', { name: 'Загрузить' }));

  await screen.findByText('Ленин');
}

describe('CutPicker', () => {
  it('показывает блоки полосы, а не сырой markdown с выделением', async () => {
    await renderPicker();
    expect(screen.getByText('Ленин')).toBeInTheDocument();
    expect(screen.getByText('писал так.')).toBeInTheDocument();
    expect(screen.queryByText('Ленин писал так.')).not.toBeInTheDocument();
  });

  it('вклейка отправляется границами блока, которые вернул сервер', async () => {
    const createCut = vi.fn().mockResolvedValue({ data: { id: 42, status: 'ok' } });
    const onInsert = vi.fn();
    await renderPicker({ createCut, onInsert });

    await userEvent.click(screen.getAllByRole('button', { name: /начало вклейки/i })[0]);
    await userEvent.click(screen.getAllByRole('button', { name: /конец вклейки/i })[0]);
    await userEvent.click(screen.getByRole('button', { name: 'Вклеить' }));

    await waitFor(() => expect(createCut).toHaveBeenCalledTimes(1));
    expect(createCut).toHaveBeenCalledWith(
      DOCUMENT_KEY,
      expect.objectContaining({ start_page: 12, start_offset: 0, end_page: 12, end_offset: 10 }),
    );
    expect(onInsert).toHaveBeenCalledWith(42);
  });

  it('собирает вклейку через две полосы: начало на одной, конец на другой', async () => {
    const createCut = vi.fn().mockResolvedValue({ data: { id: 43, status: 'ok' } });
    await renderPicker({ createCut });

    // Начало — первый блок полосы 12.
    await userEvent.click(screen.getAllByRole('button', { name: /начало вклейки/i })[0]);

    // Смена номера страницы НЕ должна сбросить уже поставленное начало —
    // это и есть жест многостраничной вклейки, а не повод для reset.
    mockedPageByNumber.mockResolvedValueOnce(ok(PAGE2) as never);
    await userEvent.clear(screen.getByLabelText(/Номер страницы/));
    await userEvent.type(screen.getByLabelText(/Номер страницы/), '13');
    await userEvent.click(screen.getByRole('button', { name: 'Загрузить' }));
    await screen.findByText('Продолжение.');

    // Кнопка «Вклеить» остаётся доступной всё это время — иначе конец на
    // другой полосе поставить нечем.
    expect(screen.getByRole('button', { name: 'Вклеить' })).toBeEnabled();

    // Конец — единственный блок полосы 13.
    await userEvent.click(screen.getAllByRole('button', { name: /конец вклейки/i })[0]);
    await userEvent.click(screen.getByRole('button', { name: 'Вклеить' }));

    await waitFor(() => expect(createCut).toHaveBeenCalledTimes(1));
    expect(createCut).toHaveBeenCalledWith(
      DOCUMENT_KEY,
      expect.objectContaining({
        start_page: 12,
        start_offset: 0,
        end_page: 13,
        end_offset: 8,
      }),
    );
  });

  it('перечисление: границы уезжают от начала первого пункта до конца последнего', async () => {
    // Замер на живом корпусе: пункты перечисления разделены пустой строкой,
    // значит блок — это ОДИН пункт, а не список целиком. Автор собирает
    // перечисление, отмечая первый пункт началом и последний — концом; тест
    // не выводит это из устройства кода (выбор краёв не различает kind), а
    // проверяет фикстурой с несколькими подряд идущими блоками вида 'list'.
    const LIST_BLOCKS: PageBlock[] = [
      { start: 0, end: 5, kind: 'list', html: '<p>Пункт 1.</p>' },
      { start: 7, end: 14, kind: 'list', html: '<p>Пункт 2.</p>' },
      { start: 16, end: 25, kind: 'list', html: '<p>Пункт 3.</p>' },
    ];
    mockedBlocks.mockReset().mockResolvedValue(ok({ blocks: LIST_BLOCKS }) as never);
    const createCut = vi.fn().mockResolvedValue({ data: { id: 50, status: 'ok' } });
    mockedCreateCut.mockImplementation(createCut as never);

    render(<CutPicker documentKey={DOCUMENT_KEY} onInsert={vi.fn()} onClose={vi.fn()} />);
    await waitFor(() => expect(screen.getByLabelText('Том')).toBeInTheDocument());
    await userEvent.selectOptions(screen.getByLabelText('Том'), ['5']);
    await waitFor(() => expect(mockedWorkGet).toHaveBeenCalledWith(5));
    await waitFor(() => expect(screen.getByLabelText(/Номер страницы/)).toBeInTheDocument());
    await userEvent.type(screen.getByLabelText(/Номер страницы/), '12');
    await userEvent.click(screen.getByRole('button', { name: 'Загрузить' }));
    await screen.findByText('Пункт 1.');

    await userEvent.click(screen.getAllByRole('button', { name: /начало вклейки/i })[0]);
    await userEvent.click(screen.getAllByRole('button', { name: /конец вклейки/i })[2]);
    await userEvent.click(screen.getByRole('button', { name: 'Вклеить' }));

    await waitFor(() => expect(createCut).toHaveBeenCalledTimes(1));
    expect(createCut).toHaveBeenCalledWith(
      DOCUMENT_KEY,
      expect.objectContaining({ start_page: 12, start_offset: 0, end_page: 12, end_offset: 25 }),
    );
  });

  it('конец раньше начала на одной и той же полосе — тот же отказ, что и между полосами, и его можно исправить', async () => {
    const createCut = vi.fn().mockResolvedValue({ data: { id: 44, status: 'ok' } });
    await renderPicker({ createCut });

    // Начало — второй блок (offset 12), конец — первый (offset 10): автор
    // перепутал порядок жестов на одной и той же полосе. Сервер отверг бы
    // это как «граница вне полосы» — сообщение про байты, не про перепутанные
    // отметки; клиент обязан сказать ровно то же, что и для перепутанных
    // полос.
    await userEvent.click(screen.getAllByRole('button', { name: /начало вклейки/i })[1]);
    await userEvent.click(screen.getAllByRole('button', { name: /конец вклейки/i })[0]);
    await userEvent.click(screen.getByRole('button', { name: 'Вклеить' }));

    expect(
      screen.getByText('Конец вклейки раньше её начала — переставьте границы.'),
    ).toBeInTheDocument();
    expect(createCut).not.toHaveBeenCalled();

    // Кнопка остаётся рабочей: автор переставляет отметки, не перезагружая
    // экран, и повторный жест отправляет вклейку как обычно.
    const submit = screen.getByRole('button', { name: 'Вклеить' });
    expect(submit).toBeEnabled();
    await userEvent.click(screen.getAllByRole('button', { name: /конец вклейки/i })[1]);
    await userEvent.click(submit);

    await waitFor(() => expect(createCut).toHaveBeenCalledTimes(1));
    expect(createCut).toHaveBeenCalledWith(
      DOCUMENT_KEY,
      expect.objectContaining({ start_page: 12, start_offset: 12, end_page: 12, end_offset: 17 }),
    );
  });

  it('подпись источника собрана той же утилитой, что и цитата', async () => {
    const createCut = vi.fn().mockResolvedValue({ data: { id: 42, status: 'ok' } });
    await renderPicker({ createCut });
    await userEvent.click(screen.getAllByRole('button', { name: /начало вклейки/i })[0]);
    await userEvent.click(screen.getAllByRole('button', { name: /конец вклейки/i })[0]);
    await userEvent.click(screen.getByRole('button', { name: 'Вклеить' }));
    await waitFor(() => expect(createCut).toHaveBeenCalled());
    const { source_title: title } = createCut.mock.calls[0][1];
    // Разделитель — запятая: он и снимает грабли joinSourceParts (edition_title
    // оканчивается точкой у настоящего корпуса, как и у WORK здесь).
    expect(title).toContain(', с. ');
    expect(title).not.toContain('.. ');
  });

  it('без отмеченных краёв показывает подсказку и не шлёт запрос', async () => {
    await renderPicker();

    await userEvent.click(screen.getByRole('button', { name: 'Вклеить' }));

    expect(screen.getByText(/Отметьте начало и конец вклейки/)).toBeInTheDocument();
    expect(mockedCreateCut).not.toHaveBeenCalled();
  });

  it('смена тома сбрасывает уже отмеченные края', async () => {
    await renderPicker();
    await userEvent.click(screen.getAllByRole('button', { name: /начало вклейки/i })[0]);
    expect(screen.getByText(/Начало: с\. 12/)).toBeInTheDocument();

    await userEvent.selectOptions(screen.getByLabelText('Том'), ['6']);
    await waitFor(() => expect(mockedWorkGet).toHaveBeenCalledWith(6));

    expect(screen.getByText(/Начало: —/)).toBeInTheDocument();
  });

  it('пустой список блоков показывает «нечего вклеить», а не пустоту', async () => {
    mockedBlocks.mockReset().mockResolvedValue(ok({ blocks: [] }) as never);
    await (async () => {
      render(<CutPicker documentKey={DOCUMENT_KEY} onInsert={vi.fn()} onClose={vi.fn()} />);
      await waitFor(() => expect(screen.getByLabelText('Том')).toBeInTheDocument());
      await userEvent.selectOptions(screen.getByLabelText('Том'), ['5']);
      await waitFor(() => expect(mockedWorkGet).toHaveBeenCalledWith(5));
      await waitFor(() => expect(screen.getByLabelText(/Номер страницы/)).toBeInTheDocument());
      await userEvent.type(screen.getByLabelText(/Номер страницы/), '12');
      await userEvent.click(screen.getByRole('button', { name: 'Загрузить' }));
    })();

    expect(await screen.findByText(/нечего вклеить/i)).toBeInTheDocument();
  });

  it('ошибка сервера при создании вклейки показывается, а не проглатывается', async () => {
    mockedCreateCut.mockRejectedValueOnce({
      response: { status: 400, data: { message: 'диапазон вне полосы' } },
    });
    await renderPicker();
    await userEvent.click(screen.getAllByRole('button', { name: /начало вклейки/i })[0]);
    await userEvent.click(screen.getAllByRole('button', { name: /конец вклейки/i })[0]);

    await userEvent.click(screen.getByRole('button', { name: 'Вклеить' }));

    await waitFor(() => expect(screen.getByText('диапазон вне полосы')).toBeInTheDocument());
  });

  it('закрывается кнопкой', async () => {
    const onClose = vi.fn();
    render(<CutPicker documentKey={DOCUMENT_KEY} onInsert={vi.fn()} onClose={onClose} />);
    await waitFor(() => expect(screen.getByLabelText('Том')).toBeInTheDocument());

    await userEvent.click(screen.getByRole('button', { name: 'закрыть' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
