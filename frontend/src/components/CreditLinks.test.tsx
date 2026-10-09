import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { CreditLinks } from './CreditLinks';

describe('CreditLinks', () => {
  it('человек — ссылкой, подпись без человека — текстом, переводчик — «пер.»', () => {
    render(
      <MemoryRouter>
        <CreditLinks
          credits={[
            {
              position: 1,
              role: 'author',
              printed: 'Гр. Баммель',
              person_id: 4,
              person_slug: 'gr-bammel',
            },
            { position: 2, role: 'author', printed: 'В. Б.' },
            { position: 3, role: 'translator', printed: 'Н. Н.' },
          ]}
        />
      </MemoryRouter>,
    );
    expect(screen.getByRole('link', { name: 'Гр. Баммель' })).toHaveAttribute(
      'href',
      '/authors/gr-bammel',
    );
    expect(screen.queryByRole('link', { name: 'В. Б.' })).toBeNull();
    expect(screen.getByText(/пер\. Н\. Н\./)).toBeInTheDocument();
  });
});
