import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import type { Chapter, PageMapEntry, Work } from '../types';
import { VolumeScale } from './VolumeScale';

const WORK = { id: 41, page_offset: 0, numbering_style: 'arabic' } as Work;

const PAGES: PageMapEntry[] = [
  { page_number: 1, status: 'не_вычитана' },
  { page_number: 2, status: 'вычитано_машиной' },
  { page_number: 3, status: 'вычитано_машиной' },
  { page_number: 4, status: 'требует_внимания' },
];

const CHAPTERS = [
  { id: 7, work_id: 41, title: 'К. Маркс. О Прудоне', start_page: 2, end_page: 4 } as Chapter,
];

/** Куда увёл переход: путь вместе с якорем — в нём и сидит номер полосы. */
function Landed({ label }: { label: string }) {
  const { pathname, hash } = useLocation();
  return (
    <div>
      {label}: {pathname}
      {hash}
    </div>
  );
}

function setup(props: Partial<React.ComponentProps<typeof VolumeScale>> = {}) {
  const onHighlight = vi.fn();
  const user = userEvent.setup();
  render(
    <MemoryRouter initialEntries={['/works/41']}>
      <Routes>
        <Route
          path="/works/41"
          element={
            <VolumeScale
              work={WORK}
              pages={PAGES}
              chapters={CHAPTERS}
              editable={false}
              highlight={null}
              onHighlight={onHighlight}
              {...props}
            />
          }
        />
        <Route path="/works/41/pages/:n" element={<Landed label="страница открыта" />} />
        <Route path="/works/41/pages/:n/edit" element={<Landed label="правка открыта" />} />
        <Route path="/works/41/chapters/:id" element={<Landed label="глава открыта" />} />
      </Routes>
    </MemoryRouter>,
  );
  return { onHighlight, user };
}

describe('VolumeScale', () => {
  it('рисует по клетке на страницу', () => {
    setup();
    expect(screen.getByRole('img', { name: 'Обрез тома' }).children).toHaveLength(4);
  });

  it('показывает в легенде только встреченные статусы с числами', () => {
    setup();
    expect(screen.getByRole('button', { name: 'вычитано машиной, 2' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'требует внимания, 1' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /вычитывается/ })).toBeNull();
  });

  it('плашка легенды включает подсветку статуса', async () => {
    const { onHighlight, user } = setup();
    await user.click(screen.getByRole('button', { name: 'требует внимания, 1' }));
    expect(onHighlight).toHaveBeenCalledWith('требует_внимания');
  });

  it('повторное нажатие на включённую плашку снимает подсветку', async () => {
    const { onHighlight, user } = setup({ highlight: 'требует_внимания' });
    await user.click(screen.getByRole('button', { name: 'требует внимания, 1' }));
    expect(onHighlight).toHaveBeenCalledWith(null);
  });

  it('переход по номеру открывает текст главы на этой странице', async () => {
    const { user } = setup();
    await user.type(screen.getByLabelText('Перейти к странице'), '3');
    await user.click(screen.getByRole('button', { name: 'Перейти' }));
    expect(screen.getByText(/глава открыта/)).toHaveTextContent(
      'глава открыта: /works/41/chapters/7#chapter-page-3',
    );
  });

  it('страница вне всех глав открывается отдельно', async () => {
    const { user } = setup();
    await user.type(screen.getByLabelText('Перейти к странице'), '1');
    await user.click(screen.getByRole('button', { name: 'Перейти' }));
    expect(screen.getByText(/страница открыта/)).toHaveTextContent(
      'страница открыта: /works/41/pages/1',
    );
  });

  it('редактору номер открывает правку полосы, а не главу', async () => {
    const { user } = setup({ editable: true });
    await user.type(screen.getByLabelText('Перейти к странице'), '3');
    await user.click(screen.getByRole('button', { name: 'Перейти' }));
    expect(screen.getByText(/правка открыта/)).toHaveTextContent(
      'правка открыта: /works/41/pages/3/edit',
    );
  });

  it('номер вне тома не уводит никуда и объясняет почему', async () => {
    const { user } = setup();
    await user.type(screen.getByLabelText('Перейти к странице'), '900');
    await user.click(screen.getByRole('button', { name: 'Перейти' }));
    expect(screen.getByRole('alert')).toHaveTextContent('В томе нет страницы 900');
    expect(screen.queryByText(/открыта/)).toBeNull();
  });
});
