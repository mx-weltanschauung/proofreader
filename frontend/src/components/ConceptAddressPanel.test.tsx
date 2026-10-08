import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

import type { Concept, ConceptArticle, ConceptReference } from '../types';
import { ConceptAddressPanel } from './ConceptAddressPanel';

function ref(over: Partial<ConceptReference>): ConceptReference {
  return {
    id: 1,
    article_id: 1,
    volume_number: 12,
    page_start: 730,
    page_end: 731,
    rubric: 'определение',
    order_number: 1,
    is_uncertain: false,
    resolved: false,
    ...over,
  };
}

const ARTICLE: ConceptArticle = {
  id: 1,
  edition_id: 1,
  edition_title: 'Сочинения',
  work_id: 40,
  source_url: '',
  title: 'Абстрактный труд',
  kind: 'article',
  article_markdown: '— определение — **12**, 730—731',
  source_page_start: 11,
  source_page_end: 11,
  references: [
    ref({ id: 1, resolved: true, work_id: 14, page_number: 730 }),
    ref({
      id: 2,
      volume_number: 23,
      page_start: 46,
      page_end: 47,
      rubric: 'его мера',
      order_number: 2,
    }),
  ],
  links: [
    {
      id: 1,
      from_article_id: 8,
      target_title: 'Труд',
      kind: 'see_also',
      order_number: 1,
      target_slug: 'trud',
    },
  ],
};

const CONCEPT: Concept = {
  id: 8,
  title: 'Абстрактный труд',
  slug: 'abstraktnyj-trud',
  sort_key: 'абстрактный труд',
  articles: [ARTICLE],
  incoming_links: [{ slug: 'stoimost', title: 'Стоимость', kind: 'see_also' }],
  created_at: '',
  updated_at: '',
};

function renderPanel(over: Partial<React.ComponentProps<typeof ConceptAddressPanel>> = {}) {
  const onFilterChange = vi.fn();
  const utils = render(
    <MemoryRouter>
      <ConceptAddressPanel
        concept={CONCEPT}
        rubric=""
        rubricPath={[]}
        volume=""
        onFilterChange={onFilterChange}
        // По умолчанию считаем адрес «т. 12 · с. 730—731» (id 1) уже
        // загруженным — так тесты, которым важна не эта развилка, а что-то
        // другое, не ломаются от появления нового обязательного пропа.
        loadedRefs={new Set([1])}
        {...over}
      />
    </MemoryRouter>,
  );
  return { ...utils, onFilterChange };
}

describe('ConceptAddressPanel', () => {
  it('перечисляет все адреса, включая неразрешённые', () => {
    renderPanel();

    expect(screen.getByRole('heading', { name: 'Адреса' })).toBeTruthy();
    expect(screen.getByText('т. 12 · с. 730—731')).toBeTruthy();
    expect(screen.getByText('т. 23 · с. 46—47')).toBeTruthy();
  });

  it('разрешённый адрес из уже загруженного окна ведёт на якорь записи', () => {
    renderPanel();

    const link = screen.getByRole('link', { name: 'т. 12 · с. 730—731' });
    expect(link.getAttribute('href')).toBe('#frag-ref-1');
  });

  it('разрешённый адрес за границей загруженного окна ведёт на страницу тома, а не в никуда', () => {
    // loadedRefs пуст: поток ещё не докачал эту порцию. До этой правки
    // ссылка на #frag-ref-1 была бы мёртвым якорем — клик ничего не делал.
    renderPanel({ loadedRefs: new Set() });

    const link = screen.getByRole('link', { name: 'т. 12 · с. 730—731' });
    expect(link.getAttribute('href')).toBe('/works/14/pages/730');
  });

  it('неразрешённый адрес остаётся не ссылкой независимо от loadedRefs', () => {
    renderPanel();

    expect(screen.queryByRole('link', { name: 'т. 23 · с. 46—47' })).toBeNull();
    expect(screen.getByText(/том не загружен/)).toBeTruthy();
  });

  it('сообщает фильтр наружу', () => {
    const { onFilterChange } = renderPanel();

    // Значение опции — закодированный путь (задача 5), а не голое имя
    // рубрики, поэтому берём его у самой опции, а не набираем строкой.
    const select = screen.getByLabelText(/подрубрика/i) as HTMLSelectElement;
    const option = [...select.querySelectorAll('option')].find(
      (o) => o.textContent === 'его мера',
    )!;
    select.value = option.value;
    select.dispatchEvent(new Event('change', { bubbles: true }));

    expect(onFilterChange).toHaveBeenCalledWith({
      rubric: '',
      rubricPath: ['его мера'],
      volume: '',
    });
  });

  it('под фильтром показывает, сколько адресов осталось', () => {
    renderPanel({ rubric: 'его мера' });

    expect(screen.getByText(/показано 1 из 2/i)).toBeTruthy();
    // Отфильтрованный адрес из панели уходит вместе с потоком.
    expect(screen.queryByText('т. 12 · с. 730—731')).toBeNull();
  });

  it('показывает печатную статью, отсылки и ссылающиеся понятия', () => {
    const { container } = renderPanel();

    expect(container.textContent).toContain('определение');
    expect(screen.getByRole('link', { name: 'Труд' }).getAttribute('href')).toBe('/concepts/trud');
    expect(screen.getByRole('link', { name: 'Стоимость' }).getAttribute('href')).toBe(
      '/concepts/stoimost',
    );
  });

  it('без обратных отсылок раздел не рисуется', () => {
    renderPanel({ concept: { ...CONCEPT, incoming_links: null } });

    expect(screen.queryByText(/ссылаются сюда/i)).toBeNull();
  });

  it('метка издания не рисуется, пока статья одна', () => {
    renderPanel();

    expect(screen.queryByText('Сочинения')).toBeNull();
  });

  it('у понятия с несколькими статьями каждая подписана своим изданием', () => {
    const second: ConceptArticle = {
      ...ARTICLE,
      id: 2,
      edition_id: 2,
      edition_title: 'Философское наследие',
      references: [ref({ id: 3, article_id: 2, volume_number: 5, rubric: 'см. также' })],
    };
    renderPanel({ concept: { ...CONCEPT, articles: [ARTICLE, second] } });

    expect(screen.getByText('Сочинения')).toBeTruthy();
    expect(screen.getByText('Философское наследие')).toBeTruthy();
  });

  it('при нескольких статьях не рисует пустую метку издания без названия', () => {
    const second: ConceptArticle = {
      ...ARTICLE,
      id: 2,
      edition_id: 2,
      edition_title: '',
      references: [ref({ id: 3, article_id: 2, volume_number: 5, rubric: 'см. также' })],
    };
    renderPanel({ concept: { ...CONCEPT, articles: [ARTICLE, second] } });

    // Первая статья подписана, у второй названия издания нет вовсе — пустого
    // абзаца вместо него быть не должно.
    const labels = document.querySelectorAll('.concept-article-edition');
    expect(labels).toHaveLength(1);
    expect(labels[0].textContent).toBe('Сочинения');
  });

  it('якорь группы различает статьи с одноимённой рубрикой', () => {
    // «определение» — обычное дело у двух указателей одного понятия. Без
    // article.id в якоре оглавление второй статьи прыгало бы к группе первой.
    const second: ConceptArticle = {
      ...ARTICLE,
      id: 2,
      edition_id: 2,
      edition_title: 'Философское наследие',
      references: [ref({ id: 3, article_id: 2, volume_number: 5, rubric: 'определение' })],
    };
    render(
      <MemoryRouter>
        <ConceptAddressPanel
          concept={{ ...CONCEPT, articles: [ARTICLE, second] }}
          rubric=""
          rubricPath={[]}
          volume=""
          onFilterChange={vi.fn()}
          loadedRefs={new Set()}
        />
      </MemoryRouter>,
    );

    // Оглавление теперь ставит фильтр, а не ссылается на якорь, но якорь
    // группы живёт ради уже разошедшихся адресов `#rubric-…`.
    const groupIds = [...document.querySelectorAll('.concept-rubric')]
      .filter((el) => el.querySelector('h3')?.textContent === 'определение')
      .map((el) => el.id);
    expect(groupIds).toHaveLength(2);
    // Разные статьи — разные якоря.
    expect(groupIds.every((id) => id.startsWith('rubric-'))).toBe(true);
    expect(new Set(groupIds).size).toBe(2);
  });
});
