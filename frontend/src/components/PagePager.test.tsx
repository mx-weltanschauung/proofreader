import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { PagePager } from './PagePager';

describe('PagePager', () => {
  it('сверху — компактный переход с местом в работе', () => {
    render(
      <MemoryRouter>
        <PagePager
          work={{ id: 41, slug: 'lenin-t06' }}
          neighbours={{ prev: 2, next: 4, index: 3, total: 908 }}
          variant="top"
        />
      </MemoryRouter>,
    );

    expect(screen.getByRole('link', { name: '← 2' })).toHaveAttribute(
      'href',
      '/works/41-lenin-t06/pages/2',
    );
    expect(screen.getByRole('link', { name: '4 →' })).toHaveAttribute(
      'href',
      '/works/41-lenin-t06/pages/4',
    );
    expect(screen.getByText('стр. 3 из 908')).toBeInTheDocument();
  });

  it('снизу — подписанные ссылки', () => {
    render(
      <MemoryRouter>
        <PagePager
          work={{ id: 41, slug: 'lenin-t06' }}
          neighbours={{ prev: 2, next: 4, index: 3, total: 908 }}
          variant="bottom"
        />
      </MemoryRouter>,
    );
    expect(screen.getByRole('link', { name: '← 2. Предыдущая' })).toHaveAttribute(
      'href',
      '/works/41-lenin-t06/pages/2',
    );
    expect(screen.getByRole('link', { name: '4. Следующая →' })).toHaveAttribute(
      'href',
      '/works/41-lenin-t06/pages/4',
    );
  });

  it('пустой слаг даёт голый номер', () => {
    render(
      <MemoryRouter>
        <PagePager
          work={{ id: 41 }}
          neighbours={{ prev: 2, next: 4, index: 3, total: 908 }}
          variant="top"
        />
      </MemoryRouter>,
    );

    expect(screen.getByRole('link', { name: '← 2' })).toHaveAttribute('href', '/works/41/pages/2');
  });

  it('на краю не рисует ссылку в никуда', () => {
    render(
      <MemoryRouter>
        <PagePager
          work={{ id: 41, slug: 'lenin-t06' }}
          neighbours={{ prev: null, next: 2, index: 1, total: 908 }}
          variant="top"
        />
      </MemoryRouter>,
    );
    expect(screen.queryByRole('link', { name: /←/ })).toBeNull();
  });
});
