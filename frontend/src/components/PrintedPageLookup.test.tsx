import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Link, MemoryRouter, useLocation } from 'react-router-dom';
import { worksApi } from '../services/api';
import type { Edition, PageMapEntry, ShelfEdition, VolumeSummary, Work } from '../types';
import { PrintedPageLookup } from './PrintedPageLookup';

const FIRST = { id: 1, title: 'Авторов. Сочинения, 2-е изд.' } as Edition;
const SECOND = { id: 2, title: 'Другов. Сочинения, 5-е изд.' } as Edition;

function volume(id: number, page_offset = 0, volume_part?: string): VolumeSummary {
  return {
    id,
    title: 'Том ' + id,
    slug: 'tom-' + id,
    volume_number: 4,
    page_offset,
    volume_part,
  } as VolumeSummary;
}

function edition(edition: Edition, ...volumes: VolumeSummary[]): ShelfEdition {
  return { edition, volumes };
}

function list(...pageNumbers: number[]): Awaited<ReturnType<typeof worksApi.pageMap>> {
  return {
    data: pageNumbers.map((page_number): PageMapEntry => ({ page_number, status: 'вычитана' })),
  } as Awaited<ReturnType<typeof worksApi.pageMap>>;
}

function CurrentPath() {
  return <div data-testid="current-path">{useLocation().pathname}</div>;
}

function LookupRoute({ shelves }: { shelves: ShelfEdition[] }) {
  return useLocation().pathname === '/' ? <PrintedPageLookup shelves={shelves} /> : null;
}

function setup(shelves: ShelfEdition[]) {
  const user = userEvent.setup();
  render(
    <MemoryRouter>
      <Link to="/documents">Разборы</Link>
      <LookupRoute shelves={shelves} />
      <CurrentPath />
    </MemoryRouter>,
  );
  const input = screen.getByLabelText('Библиографическая ссылка');
  async function submit(query: string) {
    await user.clear(input);
    await user.type(input, query);
    await user.click(screen.getByRole('button', { name: 'Найти страницу' }));
  }
  return { user, input, submit };
}

afterEach(() => vi.restoreAllMocks());

describe('Поиск по печатной ссылке', () => {
  it('находит издание по строке и открывает полосу с учётом смещения', async () => {
    const maps = vi.spyOn(worksApi, 'pageMap').mockResolvedValue(list(11));
    const { submit } = setup([edition(FIRST, volume(3)), edition(SECOND, volume(8, -1))]);
    await submit('Сочинения, 5-е изд., т. 4, с. 10');
    expect(maps.mock.calls).toEqual([[8]]);
    expect(screen.getByTestId('current-path')).toHaveTextContent('/works/8-tom-8/pages/11');
  });

  it('принимает скопированную цитату вместе с подписью и адресом', async () => {
    vi.spyOn(worksApi, 'pageMap').mockResolvedValue(list(11));
    const { input, user } = setup([edition(SECOND, volume(8, -1))]);
    await user.click(input);
    await user.paste('> Цитата\n\nСочинения, 5-е изд., т. 4, с. 10.\nhttps://example.org/source');
    await user.keyboard('{Enter}');
    expect(screen.getByTestId('current-path')).toHaveTextContent('/works/8-tom-8/pages/11');
  });

  it('предлагает выбрать издание, если названия совпадают с запросом', async () => {
    const maps = vi.spyOn(worksApi, 'pageMap').mockResolvedValue(list(10));
    const { submit, user } = setup([edition(FIRST, volume(3)), edition(SECOND, volume(8))]);
    await submit('Сочинения, т. 4, с. 10');
    expect(maps).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: FIRST.title })).toBeVisible();
    await user.click(screen.getByRole('button', { name: SECOND.title }));
    expect(maps.mock.calls).toEqual([[8]]);
    expect(screen.getByTestId('current-path')).toHaveTextContent('/works/8-tom-8/pages/10');
  });

  it('не открывает полосу, если читатель ушёл с главной до ответа', async () => {
    let resolveMap!: (result: ReturnType<typeof list>) => void;
    vi.spyOn(worksApi, 'pageMap').mockReturnValue(
      new Promise((resolve) => {
        resolveMap = resolve;
      }),
    );
    const { submit, user } = setup([edition(FIRST, volume(3))]);
    await submit('Авторов, т. 4, с. 10');
    await user.click(screen.getByRole('link', { name: 'Разборы' }));
    await act(async () => resolveMap(list(10)));
    expect(screen.getByTestId('current-path')).toHaveTextContent('/documents');
  });

  it('показывает обе части с повторной нумерацией, если обе полосы видимы', async () => {
    const maps = vi
      .spyOn(worksApi, 'pageMap')
      .mockImplementation(async (id) => (id === 3 ? list(10) : list(7)));
    const { submit } = setup([edition(FIRST, volume(3, 0, 'I'), volume(4, 3, 'II'))]);
    await submit('Авторов, т. 4, с. 10');
    expect(screen.getByRole('link', { name: /часть I ·/i })).toHaveAttribute(
      'href',
      '/works/3-tom-3/pages/10',
    );
    expect(screen.getByRole('link', { name: /часть II ·/i })).toHaveAttribute(
      'href',
      '/works/4-tom-4/pages/7',
    );

    await submit('Авторов, т. 4, ч. 2, с. 10');
    expect(maps.mock.calls).toEqual([[3], [4], [4]]);
    expect(screen.getByTestId('current-path')).toHaveTextContent('/works/4-tom-4/pages/7');
  });

  it('сохраняет строку после пустой выдачи и принимает исправление', async () => {
    vi.spyOn(worksApi, 'pageMap').mockResolvedValue(list(3, 5));
    const { input, submit } = setup([edition(FIRST, volume(3))]);
    await submit('Авторов, т. 4, с. 4');
    expect(screen.getByRole('status')).toHaveTextContent(FIRST.title);
    expect(screen.getByRole('status')).toHaveTextContent('т. 4');
    expect(input).toHaveValue('Авторов, т. 4, с. 4');
    await submit('Авторов, т. 4, с. 5');
    expect(screen.getByTestId('current-path')).toHaveTextContent('/works/3-tom-3/pages/5');
  });

  it('ищет римскую страницу в доступных предваряющих материалах тома', async () => {
    const child = {
      id: 7,
      slug: 'vvedenie',
      title: 'Введение',
      numbering_style: 'roman',
      page_offset: -2,
    } as Work;
    vi.spyOn(worksApi, 'get').mockResolvedValue({
      data: { ...volume(3), children: [child] },
    } as unknown as Awaited<ReturnType<typeof worksApi.get>>);
    const maps = vi.spyOn(worksApi, 'pageMap').mockResolvedValue(list(6));
    const { submit } = setup([edition(FIRST, volume(3))]);
    await submit('Авторов, т. 4, с. IV');
    expect(maps.mock.calls).toEqual([[7]]);
    expect(screen.getByTestId('current-path')).toHaveTextContent('/works/7-vvedenie/pages/6');
  });
});
