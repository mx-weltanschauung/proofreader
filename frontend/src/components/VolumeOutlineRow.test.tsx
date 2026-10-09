import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { Chapter, PageMapEntry, Work } from '../types';
import type { OutlineNode } from '../utils/volumeOutline';
import { VolumeOutlineRow } from './VolumeOutlineRow';

const WORK = { id: 43, page_offset: 0, numbering_style: 'arabic' } as Work;

function chapter(id: number, title: string, start: number, end: number): Chapter {
  return { id, work_id: 43, title, start_page: start, end_page: end } as Chapter;
}

function node(chapter: Chapter, over: Partial<OutlineNode> = {}): OutlineNode {
  return { chapter, level: 1, hasChildren: false, isOpen: false, children: [], ...over };
}

const RAW: PageMapEntry[] = [
  { page_number: 1, status: 'не_вычитана' },
  { page_number: 2, status: 'не_вычитана' },
];

const MIXED: PageMapEntry[] = [
  { page_number: 1, status: 'вычитана' },
  { page_number: 2, status: 'не_вычитана' },
];

function setup(target: OutlineNode, pages: PageMapEntry[] = RAW, showAuthors = false) {
  const user = userEvent.setup();
  const onToggle = vi.fn();
  const { container } = render(
    <MemoryRouter>
      <ul>
        <VolumeOutlineRow
          node={target}
          work={WORK}
          pagesInRange={() => pages}
          editable={false}
          highlight={null}
          showAuthors={showAuthors}
          onToggle={onToggle}
        />
      </ul>
    </MemoryRouter>,
  );
  return { user, onToggle, container };
}

describe('VolumeOutlineRow', () => {
  it('заглавие ведёт в чтение главы', () => {
    setup(node(chapter(1876, 'Письма без адреса', 1, 73)));
    expect(screen.getByRole('link', { name: 'Письма без адреса' })).toHaveAttribute(
      'href',
      '/works/43/chapters/1876',
    );
  });

  it('«потоком» ведёт в поток с первой страницы главы', () => {
    setup(node(chapter(1876, 'Письма без адреса', 12, 73)));
    expect(screen.getByRole('link', { name: 'Читать потоком: Письма без адреса' })).toHaveAttribute(
      'href',
      '/works/43/read/12',
    );
  });

  /*
   * Поток открывается с любой строки, а не только с листовой: адрес — это
   * страница, а глава лишь точка входа. У раздела-обёртки ссылка так же
   * осмысленна, как у письма внутри него.
   */
  it('«потоком» есть и у строки с детьми', () => {
    setup(node(chapter(1876, 'Письма без адреса', 1, 73), { hasChildren: true }));
    expect(screen.getByRole('link', { name: /Читать потоком/ })).toBeInTheDocument();
  });

  /*
   * Четыре мета-элемента лежат в одной обёртке — на ней держится перенос
   * второй строкой на узком экране (display: contents на широком, flex на
   * узком). Раскладку jsdom не считает, поэтому проверяется состав обёртки:
   * уехавший из неё элемент на телефоне окажется на чужой строке.
   */
  it('автор, действия, диапазон и полоска лежат в одной мета-обёртке', () => {
    const { container } = setup(node(chapter(1, 'Начатая', 1, 2)), MIXED, true);
    const meta = container.querySelector('.vol-toc-meta');
    expect(meta, 'мета-обёртки нет — на узком экране строка не свернётся').not.toBeNull();
    for (const cell of ['.vol-toc-author', '.vol-toc-act', '.vol-toc-pages', '.vol-toc-ready']) {
      expect(meta?.querySelector(cell), `${cell} вне обёртки`).not.toBeNull();
    }
  });

  it('автор идёт отдельно от заглавия, а не внутри ссылки', () => {
    setup(node(chapter(1, 'К. Маркс. О Прудоне', 1, 2)), RAW, true);
    expect(screen.getByRole('link', { name: 'О Прудоне' })).toBeInTheDocument();
    expect(screen.getByText('К. Маркс')).toBeInTheDocument();
  });

  it('у листа треугольника нет', () => {
    setup(node(chapter(1881, 'Генрик Ибсен', 1, 2)));
    expect(screen.queryByRole('button', { name: /Развернуть|Свернуть/ })).toBeNull();
  });

  it('треугольник узла с детьми зовёт onToggle', async () => {
    const target = node(chapter(1876, 'Письма без адреса', 1, 73), { hasChildren: true });
    const { user, onToggle } = setup(target);
    await user.click(screen.getByRole('button', { name: 'Развернуть: Письма без адреса' }));
    expect(onToggle).toHaveBeenCalledWith(target);
  });

  it('раскрытый узел зовётся «Свернуть» и объявляет aria-expanded', () => {
    setup(node(chapter(1876, 'Письма без адреса', 1, 73), { hasChildren: true, isOpen: true }));
    const button = screen.getByRole('button', { name: 'Свернуть: Письма без адреса' });
    expect(button).toHaveAttribute('aria-expanded', 'true');
  });

  it('полоски готовности нет, когда весь диапазон не вычитан', () => {
    setup(node(chapter(1, 'Нетронутая', 1, 2)), RAW);
    expect(screen.queryByRole('img')).toBeNull();
  });

  it('полоска готовности названа, когда есть что показать', () => {
    setup(node(chapter(1, 'Начатая', 1, 2)), MIXED);
    expect(screen.getByRole('img', { name: 'вычитана 1, всего 2' })).toBeInTheDocument();
  });

  it('«постранично» раскрывает клетки и работает с клавиатуры', async () => {
    const { user } = setup(node(chapter(1, 'Начатая', 1, 2)), MIXED);
    const button = screen.getByRole('button', { name: 'Постранично: Начатая' });
    expect(button).toHaveAttribute('aria-expanded', 'false');
    // Три шага табуляции: у листа треугольника нет, поэтому первым фокус
    // берёт ссылка заглавия, вторым — «потоком», третьим — «постранично».
    // Проверяется именно достижимость с клавиатуры, ради которой действия не
    // спрятаны под :hover.
    await user.tab();
    await user.tab();
    await user.tab();
    expect(button).toHaveFocus();
    await user.keyboard('{Enter}');
    expect(screen.getByRole('button', { name: 'Постранично: Начатая' })).toHaveAttribute(
      'aria-expanded',
      'true',
    );
    expect(screen.getByRole('link', { name: /стр\. 1/ })).toBeInTheDocument();
  });

  /*
   * Глава, чей диапазон страниц пуст (страницы тома до неё не доехали):
   * раскрывать нечего, и кнопки быть не должно.
   */
  it('у главы с пустым диапазоном страниц кнопки «постранично» нет', () => {
    setup(node(chapter(1, 'Пустая', 900, 910)), []);
    expect(screen.queryByRole('button', { name: /Постранично/ })).toBeNull();
  });

  /*
   * Колонка автора — свойство уровня, а не строки: если на уровне авторы
   * есть, место под них занято у КАЖДОЙ строки, даже у безымянной. Иначе
   * правый край пляшет от строки к строке — ровно то, ради чего колонка и
   * сделана свойством уровня.
   */
  it('на уровне с авторами безымянная глава оставляет колонку пустой, а не пропускает её', () => {
    const { container } = setup(node(chapter(1, 'Без автора', 1, 2)), RAW, true);
    const cell = container.querySelector('.vol-toc-author');
    expect(cell, 'колонка автора пропущена — правый край поедет').not.toBeNull();
    expect(cell).toHaveTextContent('');
  });

  it('вложенные дети рисуются вложенным списком', () => {
    const child = node(chapter(1885, 'Письмо первое', 1, 1), { level: 2 });
    setup(
      node(chapter(1876, 'Письма без адреса', 1, 73), {
        hasChildren: true,
        isOpen: true,
        children: [child],
      }),
    );
    expect(screen.getByRole('link', { name: 'Письмо первое' })).toBeInTheDocument();
    expect(screen.getAllByRole('list')).toHaveLength(2);
  });

  it('верхний уровень — заголовок третьего уровня', () => {
    setup(node(chapter(1, 'Манифест', 1, 2)));
    expect(screen.getByRole('heading', { level: 3, name: 'Манифест' })).toBeInTheDocument();
  });

  it('второй уровень — заголовок четвёртого', () => {
    setup(node(chapter(1, 'Манифест', 1, 2), { level: 2 }));
    expect(screen.getByRole('heading', { level: 4, name: 'Манифест' })).toBeInTheDocument();
  });

  /*
   * Глубже второго уровня заголовка нет вовсе: в томе 3 МиЭ дерево доходит до
   * одиннадцати уровней, и полторы сотни одноуровневых h3 делают навигацию по
   * заголовкам в скринридере бесполезной. Структуру там несут вложенные <ul>.
   */
  it('глубже второго уровня строка заголовком не объявляется', () => {
    setup(node(chapter(1, 'Фейербах', 1, 2), { level: 3 }));
    expect(screen.queryByRole('heading', { name: 'Фейербах' })).toBeNull();
    // Ссылка на месте: строка перестала быть заголовком, но не перестала
    // вести в главу.
    expect(screen.getByRole('link', { name: 'Фейербах' })).toBeInTheDocument();
  });

  it('статья с подписью и видом «рецензия»: автор ссылкой и метка вида', () => {
    const c = {
      ...chapter(5, 'О книге', 1, 2),
      article_kind: 'рецензия',
      credits: [
        { position: 1, role: 'author', printed: 'И. Рубин', person_id: 1, person_slug: 'i-rubin' },
      ],
    } as Chapter;
    setup(node(c), RAW, true);
    expect(screen.getByRole('link', { name: 'И. Рубин' })).toHaveAttribute(
      'href',
      '/authors/i-rubin',
    );
    expect(screen.getByText('рецензия')).toBeInTheDocument();
  });

  it('метка вида, совпавшая с заглавием («Выступление»), не повторяется', () => {
    const c = {
      ...chapter(7, 'Выступление', 1, 2),
      article_kind: 'выступление',
      credits: [{ position: 1, role: 'author', printed: 'А. Деборин' }],
    } as Chapter;
    const { container } = setup(node(c), RAW, true);
    expect(container.querySelector('.vol-toc-kind')).toBeNull();
    expect(screen.getByRole('link', { name: 'Выступление' })).toBeInTheDocument();
  });

  it('статья вида «статья»: метки нет', () => {
    const c = {
      ...chapter(6, 'Статья', 1, 2),
      article_kind: 'статья',
      credits: [{ position: 1, role: 'author', printed: 'И. Рубин' }],
    } as Chapter;
    const { container } = setup(node(c), RAW, true);
    expect(container.querySelector('.vol-toc-kind')).toBeNull();
    expect(screen.getByText('И. Рубин')).toBeInTheDocument();
  });
});
