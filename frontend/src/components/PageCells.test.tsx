import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { PageMapEntry, Work } from '../types';
import { PageCells } from './PageCells';

const WORK = { id: 41, page_offset: 0, numbering_style: 'arabic' } as Work;

const PAGES: PageMapEntry[] = [
  { page_number: 3, status: 'не_вычитана' },
  { page_number: 4, status: 'вычитано_машиной' },
];

function setup(props: Partial<React.ComponentProps<typeof PageCells>> = {}) {
  return render(
    <MemoryRouter>
      <PageCells work={WORK} pages={PAGES} editable={false} {...props} />
    </MemoryRouter>,
  );
}

describe('PageCells', () => {
  it('рисует клетку на каждую страницу с подписью статуса', () => {
    setup();
    expect(screen.getByRole('link', { name: 'стр. 3, не вычитана' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'стр. 4, вычитано машиной' })).toBeInTheDocument();
  });

  it('ведёт гостя на страницу, а редактора — в правку', () => {
    setup();
    expect(screen.getByRole('link', { name: /стр\. 3/ })).toHaveAttribute(
      'href',
      '/works/41/pages/3',
    );
    setup({ editable: true });
    expect(screen.getAllByRole('link', { name: /стр\. 3/ })[1]).toHaveAttribute(
      'href',
      '/works/41/pages/3/edit',
    );
  });

  it('печатает колонцифру, когда счёт расходится с адресным', () => {
    setup({ work: { ...WORK, page_offset: 10 } as Work });
    expect(
      screen.getByRole('link', { name: 'стр. 3, печатная 13, не вычитана' }),
    ).toBeInTheDocument();
  });

  it('гасит клетки прочих статусов при подсветке', () => {
    setup({ highlight: 'вычитано_машиной' });
    expect(screen.getByRole('link', { name: /стр\. 3/ })).toHaveClass('is-dimmed');
    expect(screen.getByRole('link', { name: /стр\. 4/ })).not.toHaveClass('is-dimmed');
  });

  it('ничего не рисует на пустом диапазоне', () => {
    const { container } = setup({ pages: [] });
    expect(container).toBeEmptyDOMElement();
  });
});
