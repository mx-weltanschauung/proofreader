import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { useCanonicalPath } from './useCanonicalPath';

function Probe({ canonical }: { canonical: string | null }) {
  useCanonicalPath(canonical);
  const loc = useLocation();
  return <span data-testid="here">{loc.pathname + loc.search + loc.hash}</span>;
}

describe('useCanonicalPath', () => {
  it('подменяет числовой адрес каноном', () => {
    const { getByTestId } = render(
      <MemoryRouter initialEntries={['/works/49']}>
        <Probe canonical="/works/49-lenin-t06" />
      </MemoryRouter>,
    );
    expect(getByTestId('here').textContent).toBe('/works/49-lenin-t06');
  });

  it('сохраняет строку запроса и хеш', () => {
    // ?q= несёт подсветку поиска, хеш — якорь подглавы. Потеря любого из них
    // сломала бы ровно тот заход, ради которого ссылку и присылают.
    const { getByTestId } = render(
      <MemoryRouter initialEntries={['/works/49/chapters/10125?q=Гегель#subchapter-3']}>
        <Probe canonical="/works/49-lenin-t06/chapters/10125-chto-delat" />
      </MemoryRouter>,
    );
    expect(getByTestId('here').textContent).toBe(
      '/works/49-lenin-t06/chapters/10125-chto-delat?q=Гегель#subchapter-3',
    );
  });

  it('канонический адрес не трогает', () => {
    const { getByTestId } = render(
      <MemoryRouter initialEntries={['/works/49-lenin-t06']}>
        <Probe canonical="/works/49-lenin-t06" />
      </MemoryRouter>,
    );
    expect(getByTestId('here').textContent).toBe('/works/49-lenin-t06');
  });
});
