import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { ConceptFragmentBlock } from './ConceptFragmentBlock';
import { conceptsApi } from '../services/api';
import type { ConceptEntry, ConceptCut } from '../types';

// Подменяется ровно expandPage, остальной модуль остаётся настоящим: компонент
// тянет за собой useNoteXrefs → services/notesIndex → services/api, и заглушка
// на весь модуль оставила бы их с undefined вместо клиентов. vi.fn() создаётся
// внутри фабрики, а ссылка берётся через vi.mocked после импорта: vi.mock
// поднимается выше объявлений модуля, и внешняя const в фабрике была бы ещё не
// инициализирована (тот же приём — в ConceptEntryExpand.test.tsx).
vi.mock('../services/api', async () => {
  const actual = await vi.importActual<typeof import('../services/api')>('../services/api');
  return { ...actual, conceptsApi: { ...actual.conceptsApi, expandPage: vi.fn() } };
});

const expandPage = vi.mocked(conceptsApi.expandPage);

beforeEach(() => {
  expandPage.mockReset();
  expandPage.mockResolvedValue({
    data: {
      page_id: 9144,
      page_number: 731,
      printed_page: 731,
      page_status: 'не_вычитана',
      markdown: 'продолжение',
      chunks: [{ html: '<p>продолжение</p>', inside: false }],
    },
  } as never);
});

function cut(over: Partial<ConceptCut> = {}): ConceptCut {
  return {
    id: 91,
    bounds: { start_page_id: 9143, start_offset: 0, end_page_id: 9143, end_offset: 20 },
    status: 'machine',
    head_quote: 'Текст вырезки',
    parts: [
      {
        page_id: 9143,
        page_number: 730,
        printed_page: 730,
        page_status: 'не_вычитана',
        html: '<p>Текст вырезки</p>',
      },
    ],
    ...over,
  };
}

function entry(over: Partial<ConceptEntry> = {}): ConceptEntry {
  return {
    reference_id: 473,
    volume_number: 12,
    printed_start: 730,
    printed_end: 731,
    work_id: 14,
    work_title: 'Экономические рукописи',
    chapter_title: 'Введение',
    chapter_id: null,
    rubric: 'определение',
    rubric_path: ['определение'],
    is_uncertain: false,
    state: 'fragment',
    pages: [
      { page_id: 9143, page_number: 730, printed_page: 730, page_status: 'не_вычитана' },
      { page_id: 9144, page_number: 731, printed_page: 731, page_status: 'не_вычитана' },
    ],
    cuts: [cut()],
    stale_cuts: [],
    ...over,
  };
}

function renderEntry(e: ConceptEntry, showRubric = false) {
  return render(
    <MemoryRouter>
      <ConceptFragmentBlock entry={e} slug="abstraktnyj-trud" showRubric={showRubric} />
    </MemoryRouter>,
  );
}

describe('ConceptFragmentBlock', () => {
  it('шапка называет подрубрику, адрес, работу и главу', () => {
    renderEntry(entry(), true);

    expect(screen.getByText('определение')).toBeInTheDocument();
    expect(screen.getByText(/т\. 12 · с\. 730—731/)).toBeInTheDocument();
    expect(screen.getByText(/Экономические рукописи/)).toBeInTheDocument();
    expect(screen.getByText(/Введение/)).toBeInTheDocument();
  });

  it('название главы — ссылка на главу тома', () => {
    renderEntry(entry({ work_slug: 'mae-t46-1', chapter_id: 7, chapter_slug: 'vvedenie' }));
    expect(screen.getByRole('link', { name: 'Введение' })).toHaveAttribute(
      'href',
      '/works/14-mae-t46-1/chapters/7-vvedenie',
    );
  });

  it('глава без id остаётся текстом', () => {
    renderEntry(entry({ chapter_id: null }));
    expect(screen.getByText(/Введение/)).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Введение' })).toBeNull();
  });

  it('при showRubric={false} подрубрика в шапке не рисуется', () => {
    const { container } = renderEntry(entry(), false);
    // По классу, а не по тексту: текст подрубрики может встретиться и в теле
    // вырезки — проверяем именно узел шапки.
    expect(container.querySelector('.concept-fragment-rubrics')).toBeNull();
  });

  it('якорь строится по адресу', () => {
    const { container } = renderEntry(entry());
    expect(container.querySelector('#frag-ref-473')).not.toBeNull();
  });

  it('статус страницы показан, кроме «вычитана»', () => {
    renderEntry(entry());
    expect(screen.getByText('не вычитана')).toBeInTheDocument();

    const proofread = entry({
      cuts: [cut({ parts: [{ ...cut().parts[0], page_status: 'вычитана' }] })],
    });
    const { container } = renderEntry(proofread);
    // У сверенной человеком страницы добавить нечего — пометки нет.
    expect(container.querySelectorAll('.concept-cut-status')).toHaveLength(0);
  });

  it('ненарезанный адрес помечен', () => {
    renderEntry(
      entry({
        state: 'whole_page',
        cuts: [
          cut({
            id: null,
            bounds: null,
            status: 'whole_page',
            parts: [
              cut().parts[0],
              {
                page_id: 9144,
                page_number: 731,
                printed_page: 731,
                page_status: 'не_вычитана',
                html: '<p>Продолжение</p>',
              },
            ],
          }),
        ],
      }),
    );
    expect(screen.getByText(/фрагмент не выделен/)).toBeInTheDocument();
  });

  it('отвязавшийся адрес помечен', () => {
    renderEntry(
      entry({
        state: 'stale',
        cuts: [
          cut({
            id: null,
            bounds: null,
            status: 'stale',
            parts: [
              cut().parts[0],
              {
                page_id: 9144,
                page_number: 731,
                printed_page: 731,
                page_status: 'не_вычитана',
                html: '<p>Продолжение</p>',
              },
            ],
          }),
        ],
      }),
    );
    expect(screen.getByText(/границы съехали/)).toBeInTheDocument();
  });

  // Находка 1 финального разбора: часть вырезок адреса отвязалась (правка
  // другой его страницы), а часть осталась живой — запись остаётся в
  // состоянии fragment (её живая вырезка показывается как обычно), но
  // читатель должен видеть, что адрес неполон, а не решить, что вырезка на
  // отвязавшейся странице просто не существовала никогда.
  it('смешанный адрес (живая и отвязавшаяся вырезки) показывает и текст, и пометку', () => {
    renderEntry(
      entry({
        state: 'fragment',
        cuts: [cut()],
        stale_cuts: [
          {
            id: 91,
            bounds: { start_page_id: 9144, start_offset: 0, end_page_id: 9144, end_offset: 5 },
            status: 'stale',
            head_quote: '',
            parts: [],
          },
        ],
      }),
    );

    // Живая часть по-прежнему видна.
    expect(screen.getByText('Текст вырезки')).toBeInTheDocument();
    // И пометка о неполноте адреса — рядом, а не молчание.
    expect(screen.getByText(/часть вырезок адреса отвязалась/)).toBeInTheDocument();
  });

  // Cheap fix финального разбора: живая вырезка без частей (якорь вне
  // текущего диапазона адреса, см. TestFragmentsLiveCutWithVanishedAnchorsHasEmptyPartsNotNull
  // на сервере) раньше рендерилась совсем пусто — ни текста, ни кнопки, ни
  // пометки, ни ссылки. Теперь у неё должен быть видимый откат: пометка и
  // кнопка развернуть страницу целиком по known bounds.start_page_id.
  it('живая вырезка без частей не рендерится пусто — есть пометка и кнопка развернуть', () => {
    const { container } = renderEntry(
      entry({
        cuts: [
          {
            id: 91,
            bounds: { start_page_id: 9999, start_offset: 0, end_page_id: 9999, end_offset: 5 },
            status: 'machine',
            head_quote: '',
            parts: [],
          },
        ],
      }),
    );

    expect(screen.getByText(/текст вырезки недоступен/)).toBeInTheDocument();
    expect(container.querySelector('.concept-cut-expand')).not.toBeNull();
  });

  it('вырезка через границу страниц разделена фолио', () => {
    const across = entry({
      cuts: [
        cut({
          parts: [
            cut().parts[0],
            {
              page_id: 9144,
              page_number: 731,
              printed_page: 731,
              page_status: 'вычитана',
              html: '<p>Продолжение</p>',
            },
          ],
        }),
      ],
    });
    renderEntry(across);

    // Мысль идёт через разрыв страницы; читать её надо так же, с колонцифрой
    // на переходе.
    expect(screen.getByText('· с. 731 ·')).toBeInTheDocument();
  });

  it('две вырезки одного адреса разделены пропуском', () => {
    const two = entry({
      cuts: [cut(), cut({ id: 92, parts: [{ ...cut().parts[0], html: '<p>Второе место</p>' }] })],
    });
    const { container } = renderEntry(two);

    expect(container.querySelectorAll('.concept-cut')).toHaveLength(2);
    expect(container.querySelectorAll('.concept-cut-gap')).toHaveLength(1);
  });

  // Адрес указателя шире своих вырезок: 730—731, вырезка на 730-й. Без
  // заглушки 731-й страницы в записи не существует — читатель не узнает, что
  // смотреть надо и там, а редактор не сможет её открыть и нарезать.
  it('ненарезанная страница адреса показана заглушкой с разворотом', () => {
    const { container } = renderEntry(entry());

    const uncut = container.querySelector('.concept-cut-uncut');
    expect(uncut).not.toBeNull();
    expect(uncut!.textContent).toContain('· с. 731 ·');
    expect(uncut!.textContent).toContain('фрагмент не выделен');
    expect(uncut!.querySelector('.concept-cut-expand')).not.toBeNull();
  });

  it('заглушек нет, когда вырезки покрыли все страницы адреса', () => {
    const across = entry({
      cuts: [
        cut({
          parts: [
            cut().parts[0],
            {
              page_id: 9144,
              page_number: 731,
              printed_page: 731,
              page_status: 'не_вычитана',
              html: '<p>Продолжение</p>',
            },
          ],
        }),
      ],
    });
    const { container } = renderEntry(across);

    expect(container.querySelectorAll('.concept-cut-uncut')).toHaveLength(0);
  });

  // Порядок блоков — порядок страниц, а не порядок вырезок в ответе: пустая
  // 730-я стоит на бумаге раньше нарезанной 731-й, и в записи должна стоять
  // раньше.
  it('заглушка встаёт по номеру страницы, а не в конец', () => {
    const cutOn731 = entry({
      cuts: [
        cut({
          bounds: { start_page_id: 9144, start_offset: 0, end_page_id: 9144, end_offset: 20 },
          parts: [
            {
              page_id: 9144,
              page_number: 731,
              printed_page: 731,
              page_status: 'не_вычитана',
              html: '<p>Текст вырезки</p>',
            },
          ],
        }),
      ],
    });
    const { container } = renderEntry(cutOn731);

    const blocks = container.querySelectorAll('.concept-cut, .concept-cut-uncut');
    expect(blocks).toHaveLength(2);
    expect(blocks[0].className).toContain('concept-cut-uncut');
  });

  // «⋯» означает «между двумя вырезками пропущен текст». Заглушка сама и есть
  // видимый разрыв, к тому же подписанный колонцифрой, — второй знак рядом с
  // ней только сбивал бы.
  it('рядом с заглушкой пропуск «⋯» не рисуется', () => {
    const { container } = renderEntry(entry());

    expect(container.querySelectorAll('.concept-cut-gap')).toHaveLength(0);
  });

  it('две вырезки на разных страницах подписаны колонцифрой, включая первую', () => {
    const twoOnDifferentPages = entry({
      cuts: [
        cut(), // первая вырезка на странице 730
        cut({
          id: 92,
          bounds: { start_page_id: 9144, start_offset: 0, end_page_id: 9144, end_offset: 20 },
          parts: [
            {
              page_id: 9144,
              page_number: 731,
              printed_page: 731,
              page_status: 'не_вычитана',
              html: '<p>Вторая вырезка</p>',
            },
          ],
        }),
      ],
    });
    renderEntry(twoOnDifferentPages);

    // Обе вырезки должны быть подписаны колонцифрой — и первая, и вторая.
    // Это показывает читателю, что они на разных страницах.
    expect(screen.getByText('· с. 730 ·')).toBeInTheDocument();
    expect(screen.getByText('· с. 731 ·')).toBeInTheDocument();
  });

  // Головная цитата вырезки уже поддерживается живым переякориванием — из
  // неё строится адрес МЕСТА, а не всей полосы: ?quote= ищется на открытой
  // странице и подсвечивает ровно то, что показано в вырезке указателя.
  it('ссылка вырезки ведёт на её место, а не на всю полосу', () => {
    renderEntry(entry());

    const link = screen.getByRole('link', { name: /открыть страницу тома/ });
    expect(link).toHaveAttribute(
      'href',
      `/works/14/pages/730?quote=${encodeURIComponent('Текст вырезки')}`,
    );
  });

  // У синтетической/отвязавшейся вырезки якоря нет (head_quote пуст) — вести
  // читателя на конкретное место в тексте, не подтверждённое переякориванием,
  // значило бы вести его наугад. Ссылка обязана остаться голым адресом полосы.
  it('у отвязавшейся вырезки внешней ссылки на место нет', () => {
    renderEntry(entry({ cuts: [cut({ head_quote: '' })] }));

    const link = screen.getByRole('link', { name: /открыть страницу тома/ });
    expect(link).toHaveAttribute('href', '/works/14/pages/730');
  });

  it('кнопка заглушки разворачивает именно её страницу', async () => {
    const user = userEvent.setup();
    const { container } = renderEntry(entry());

    await user.click(container.querySelector('.concept-cut-uncut .concept-cut-expand')!);

    expect(expandPage).toHaveBeenCalledWith('abstraktnyj-trud', 473, 9144);
  });
});
