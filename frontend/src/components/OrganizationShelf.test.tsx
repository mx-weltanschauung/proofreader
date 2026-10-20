import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { OrganizationShelf } from './OrganizationShelf';

describe('OrganizationShelf', () => {
  it('заголовок раздела, число событий и ссылка на организацию', () => {
    render(
      <MemoryRouter>
        <OrganizationShelf
          organizations={[{ id: 1, slug: 'org-a', title: 'Организация А', events_total: 3 }]}
        />
      </MemoryRouter>,
    );
    expect(screen.getByRole('heading', { name: 'Съезды и конгрессы' })).toBeInTheDocument();
    expect(screen.getByText(/3 события/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Организация А/ })).toHaveAttribute(
      'href',
      '/organizations/org-a',
    );
  });

  it('пустой список ничего не рисует', () => {
    const { container } = render(
      <MemoryRouter>
        <OrganizationShelf organizations={[]} />
      </MemoryRouter>,
    );
    expect(container).toBeEmptyDOMElement();
  });
});
