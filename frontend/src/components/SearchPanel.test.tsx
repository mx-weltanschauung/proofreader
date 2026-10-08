import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { SearchPanel } from './SearchPanel';
import { EMPTY_SCOPE } from '../utils/searchScope';

const shelf = {
  editions: [
    {
      edition: { id: 2, title: 'К. Маркс и Ф. Энгельс. Сочинения' },
      volumes: [{ id: 13, title: 'Том 13', volume_number: 13 }],
    },
  ],
  loose_works: [],
};

vi.mock('../services/api', () => ({
  shelfApi: { get: vi.fn(() => Promise.resolve({ data: shelf })) },
}));

function Probe() {
  const location = useLocation();
  return <output data-testid="loc">{location.pathname + location.search}</output>;
}

function renderPanel(props: Partial<React.ComponentProps<typeof SearchPanel>> = {}) {
  const onClose = vi.fn();
  render(
    <MemoryRouter initialEntries={['/works/7']}>
      <Routes>
        <Route
          path="*"
          element={
            <SearchPanel
              open
              initialQuery=""
              initialScope={EMPTY_SCOPE}
              onClose={onClose}
              {...props}
            />
          }
        />
      </Routes>
      <Probe />
    </MemoryRouter>,
  );
  return { onClose };
}

describe('SearchPanel', () => {
  it('открывается диалогом, фокус в поле запроса', () => {
    renderPanel();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByRole('searchbox')).toHaveFocus();
  });

  it('Enter уводит на выдачу и закрывает панель', async () => {
    const { onClose } = renderPanel();
    await userEvent.type(screen.getByRole('searchbox'), 'партия{Enter}');
    expect(screen.getByTestId('loc')).toHaveTextContent('/search?q=');
    expect(onClose).toHaveBeenCalled();
  });

  it('Escape закрывает панель, никуда не уводя', async () => {
    const { onClose } = renderPanel();
    await userEvent.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalled();
    expect(screen.getByTestId('loc')).toHaveTextContent('/works/7');
  });

  it('однобуквенный запрос объясняет отказ и остаётся на месте', async () => {
    renderPanel();
    await userEvent.type(screen.getByRole('searchbox'), 'и{Enter}');
    expect(screen.getByRole('alert')).toHaveTextContent('не меньше двух символов');
    expect(screen.getByTestId('loc')).toHaveTextContent('/works/7');
  });

  it('печатает приёмы запроса', () => {
    renderPanel();
    expect(screen.getByText('"что делать"')).toBeInTheDocument();
  });

  it('недавний запрос подставляется в поле', async () => {
    localStorage.setItem('reading-room.recent-queries', JSON.stringify(['стоимость']));
    renderPanel();
    await userEvent.click(screen.getByRole('button', { name: 'стоимость' }));
    expect(screen.getByRole('searchbox')).toHaveValue('стоимость');
  });

  it('выбранная область уезжает в адрес выдачи', async () => {
    renderPanel();
    await userEvent.click(await screen.findByRole('checkbox', { name: /Маркс/ }));
    await userEvent.type(screen.getByRole('searchbox'), 'партия{Enter}');
    expect(screen.getByTestId('loc')).toHaveTextContent('editions=2');
  });

  // Круг правок 1, находка 3: поведение унаследовано от удалённого
  // SearchForm.test.tsx («пустой запрос никуда не ведёт») и не было
  // перенесено вместе с формой — код (`if (!q) return;`) остался, но без
  // теста ничем не защищён.
  it('пустой запрос никуда не ведёт', async () => {
    const { onClose } = renderPanel();
    await userEvent.type(screen.getByRole('searchbox'), '{Enter}');
    expect(screen.getByTestId('loc')).toHaveTextContent('/works/7');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });

  // Круг правок 1, находка 3: второе унаследованное поведение — дописанная
  // вторая буква снимает отказ ДО того, как читатель нажал Enter снова (сам
  // onChange чистит tooShort), и это доводит запрос до выдачи.
  it('дописанная вторая буква снимает отказ и уводит на выдачу', async () => {
    const { onClose } = renderPanel();
    const input = screen.getByRole('searchbox');
    await userEvent.type(input, 'и{Enter}');
    expect(screen.getByRole('alert')).toBeInTheDocument();
    await userEvent.type(input, 'т');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    await userEvent.keyboard('{Enter}');
    expect(screen.getByTestId('loc')).toHaveTextContent('/search?q=');
    expect(onClose).toHaveBeenCalled();
  });
});
