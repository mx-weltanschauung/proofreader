import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { HomeToc } from './HomeToc';
import { buildHomeLayout } from '../utils/homeLayout';
import type { Edition, EventOrganizationSummary, JournalSummary, VolumeSummary } from '../types';

function volume(id: number): VolumeSummary {
  return {
    id,
    title: `том ${id}`,
    volume_number: id,
    pages_total: 100,
    pages_by_status: {},
    chapters_total: 0,
  } as VolumeSummary;
}

function edition(id: number, title: string, over: Partial<Edition> = {}): Edition {
  return {
    id,
    title,
    slug: `ed-${id}`,
    url_slug: `e${id}`,
    description: '',
    created_at: '',
    updated_at: '',
    ...over,
  };
}

const JOURNAL = { id: 5, slug: 'zh', title: 'Журнал А', issues_total: 3 } as JournalSummary;
const ORG_IN: EventOrganizationSummary = {
  id: 1,
  slug: 'org-a',
  title: 'Организация А',
  events_total: 21,
  shelf_group: 'Раздел Б',
};
const ORG_OUT: EventOrganizationSummary = {
  id: 2,
  slug: 'org-b',
  title: 'Организация Б',
  events_total: 2,
};

function renderToc(layout: ReturnType<typeof buildHomeLayout>) {
  return render(
    <MemoryRouter>
      <HomeToc layout={layout} />
    </MemoryRouter>,
  );
}

const titles = () =>
  Array.from(
    screen
      .getByRole('navigation', { name: 'Оглавление читальни' })
      .querySelectorAll('.home-toc-title'),
  ).map((el) => el.textContent);

describe('HomeToc', () => {
  it('колонки идут в порядке раскладки', () => {
    renderToc(
      buildHomeLayout(
        [
          { edition: edition(1, 'А. Автор. Сочинения'), volumes: [volume(1)] },
          {
            edition: edition(2, 'Раздел Б. Отчёты', { shelf_group: 'Раздел Б' }),
            volumes: [volume(1), volume(2)],
          },
        ],
        [ORG_IN, ORG_OUT],
        [JOURNAL],
        [{ id: 9, title: 'Брошюра' }],
      ),
    );
    expect(titles()).toEqual([
      'Собрания сочинений',
      'Раздел Б',
      'Журналы',
      'Съезды и конгрессы',
      'Вне собраний',
    ]);
  });

  it('строка собрания — автор жирным, остаток, число томов, ссылка на полку', () => {
    renderToc(
      buildHomeLayout(
        [
          {
            edition: edition(1, 'Г. В. Ф. Гегель. Сочинения в 14 томах'),
            volumes: [volume(1), volume(2)],
          },
        ],
        [],
        [],
        [],
      ),
    );
    const link = screen.getByRole('link', { name: /Гегель/ });
    expect(link).toHaveAttribute('href', '#ed-e1');
    expect(within(link).getByText('Г. В. Ф. Гегель').tagName).toBe('B');
    expect(within(link).getByText('Сочинения в 14 томах')).toHaveClass('home-toc-rest');
    expect(link.closest('li')).toHaveTextContent('2 тома');
  });

  it('в именованном разделе — короткое заглавие и организация последней строкой', () => {
    renderToc(
      buildHomeLayout(
        [
          {
            edition: edition(2, 'Раздел Б. Отчёты', { shelf_group: 'Раздел Б' }),
            volumes: [volume(1)],
          },
        ],
        [ORG_IN],
        [],
        [],
      ),
    );
    const items = screen.getAllByRole('listitem');
    expect(items.map((li) => li.textContent)).toEqual(['Отчёты1 том', 'Организация А21 событие']);
    expect(within(items[1]).getByRole('link')).toHaveAttribute('href', '#org-org-a');
  });

  it('закрытое собрание сотруднику помечено', () => {
    renderToc(
      buildHomeLayout(
        [{ edition: edition(1, 'Закрытое', { closed: true }), volumes: [volume(1)] }],
        [],
        [],
        [],
      ),
    );
    expect(screen.getByText('закрыт')).toBeInTheDocument();
  });

  it('без работ вне собраний нет и колонки', () => {
    renderToc(buildHomeLayout([{ edition: edition(1, 'X'), volumes: [volume(1)] }], [], [], []));
    expect(titles()).toEqual(['Собрания сочинений']);
  });

  it('пустая раскладка — оглавления нет', () => {
    renderToc(buildHomeLayout([], [], [], []));
    expect(screen.queryByRole('navigation', { name: 'Оглавление читальни' })).toBeNull();
  });
});
