import { describe, it, expect } from 'vitest';
import userEvent from '@testing-library/user-event';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { Chapter } from '../types';
import { ChapterBreadcrumb } from './ChapterBreadcrumb';

function ch(id: number, title: string, orderNumber: number): Chapter {
  return {
    id,
    work_id: 4,
    parent_id: null,
    title,
    type: 'chapter',
    order_number: orderNumber,
    start_page: 1,
    end_page: 1,
    is_apparatus: false,
    created_at: '',
    updated_at: '',
  };
}

const LEVELS: Chapter[][] = [
  [ch(230, 'Нищета философии', 20)],
  [ch(309, '§ I. Метод', 70)],
  [ch(320, 'Замечание второе', 81), ch(321, 'Замечание третье', 82)],
];

function renderBreadcrumb(
  levels: Chapter[][],
  work: { id: number; slug?: string } | undefined = { id: 4 },
) {
  return render(
    <MemoryRouter>
      <ChapterBreadcrumb work={work} levels={levels} />
    </MemoryRouter>,
  );
}

describe('ChapterBreadcrumb', () => {
  it('делает каждую главу ссылкой на её страницу', () => {
    renderBreadcrumb(LEVELS);

    expect(screen.getByRole('link', { name: 'Нищета философии' })).toHaveAttribute(
      'href',
      '/works/4/chapters/230',
    );
    expect(screen.getByRole('link', { name: '§ I. Метод' })).toHaveAttribute(
      'href',
      '/works/4/chapters/309',
    );
    expect(screen.getByRole('link', { name: 'Замечание третье' })).toHaveAttribute(
      'href',
      '/works/4/chapters/321',
    );
  });

  it('разделяет уровни слэшем, а главы одного уровня — точкой', () => {
    const { container } = renderBreadcrumb(LEVELS);

    const separators = [...container.querySelectorAll('.chapter-breadcrumb-separator')].map(
      (node) => node.textContent?.trim(),
    );

    expect(separators).toEqual(['/', '/', '·']);
  });

  it('ничего не рендерит без глав', () => {
    const { container } = renderBreadcrumb([]);

    expect(container).toBeEmptyDOMElement();
  });

  it('ничего не рендерит без work', () => {
    // Мимо хелпера: у его параметра есть дефолт { id: 4 }, и явный undefined
    // этот дефолт не отменяет, а включает — тест проверял бы не то, что
    // заявляет.
    const { container } = render(
      <MemoryRouter>
        <ChapterBreadcrumb work={undefined} levels={LEVELS} />
      </MemoryRouter>,
    );

    expect(container).toBeEmptyDOMElement();
  });
});

// Уровни строит chapterLevelsForPage; здесь важно только их количество.
function levelsOfDepth(depth: number): Chapter[][] {
  return Array.from({ length: depth }, (_, i) => [ch(100 + i, `Уровень ${i}`, i)]);
}

describe('ChapterBreadcrumb: свёртка длинной цепочки', () => {
  it('не сворачивает цепочку из четырёх уровней', () => {
    renderBreadcrumb(levelsOfDepth(4));

    expect(screen.getAllByRole('link')).toHaveLength(4);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('сворачивает середину цепочки из одиннадцати уровней', () => {
    renderBreadcrumb(levelsOfDepth(11));

    // Видны первый и два последних уровня.
    expect(screen.getAllByRole('link').map((node) => node.textContent)).toEqual([
      'Уровень 0',
      'Уровень 9',
      'Уровень 10',
    ]);
    // Восемь скрытых уровней названы числом, а не «несколько».
    expect(screen.getByRole('button')).toHaveAccessibleName(/8/);
  });

  it('раскрывает полный путь по клику и убирает кнопку', async () => {
    const user = userEvent.setup();
    renderBreadcrumb(levelsOfDepth(11));

    await user.click(screen.getByRole('button'));

    expect(screen.getAllByRole('link')).toHaveLength(11);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('снова сворачивает цепочку при переходе на другую страницу', async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <MemoryRouter>
        <ChapterBreadcrumb work={{ id: 3 }} levels={levelsOfDepth(11)} />
      </MemoryRouter>,
    );

    await user.click(screen.getByRole('button'));
    expect(screen.getAllByRole('link')).toHaveLength(11);

    // Новый массив уровней = новые данные = новая страница.
    rerender(
      <MemoryRouter>
        <ChapterBreadcrumb work={{ id: 3 }} levels={levelsOfDepth(11)} />
      </MemoryRouter>,
    );

    expect(screen.getAllByRole('link')).toHaveLength(3);
    expect(screen.queryByRole('button')).toBeInTheDocument();
  });

  it('сохраняет сестринские главы в видимых уровнях', () => {
    const levels = levelsOfDepth(11);
    levels[10] = [ch(900, 'Замечание второе', 81), ch(901, 'Замечание третье', 82)];

    const { container } = renderBreadcrumb(levels);

    const separators = [...container.querySelectorAll('.chapter-breadcrumb-separator')].map(
      (node) => node.textContent?.trim(),
    );

    // Уровень 0 / … / Уровень 9 / Замечание второе · Замечание третье
    expect(separators).toEqual(['/', '/', '/', '·']);
  });
});
