import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { SearchTrigger } from './SearchTrigger';

const shelf = {
  editions: [
    {
      edition: { id: 2, title: 'К. Маркс и Ф. Энгельс. Сочинения' },
      volumes: [{ id: 7, title: 'Том 7', volume_number: 7 }],
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

describe('SearchTrigger', () => {
  it('нажатие открывает панель', async () => {
    render(
      <MemoryRouter>
        <SearchTrigger compact />
      </MemoryRouter>,
    );
    await userEvent.click(screen.getByRole('button', { name: /поиск/i }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('область страницы приезжает предвыбранной', async () => {
    render(
      <MemoryRouter>
        <SearchTrigger scope={{ editions: [], works: [7], chapters: [] }} label="Искать в томе" />
        <Probe />
      </MemoryRouter>,
    );
    await userEvent.click(screen.getByRole('button', { name: /искать в томе/i }));
    await userEvent.type(screen.getByRole('searchbox'), 'партия{Enter}');
    expect(screen.getByTestId('loc')).toHaveTextContent('works=7');
  });

  it('клавиша / открывает панель с любого места страницы', async () => {
    render(
      <MemoryRouter>
        <SearchTrigger compact />
      </MemoryRouter>,
    );
    await userEvent.keyboard('/');
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  // Находка 1 итоговой рецензии: на одной странице затравок бывает сразу
  // две-три (шапка + карточка тома/собрания + строка области на /search) —
  // каждая раньше вешала свой слушатель, и «/» открывал столько же панелей
  // разом, с разной предвыбранной областью. Здесь их две: пустая (как в
  // шапке) и с областью тома (как на карточке тома) — «/» обязан открыть
  // РОВНО одну, и это должна быть та, у которой область конкретнее.
  it('клавиша / с несколькими затравками на странице открывает ровно одну панель', async () => {
    render(
      <MemoryRouter>
        <SearchTrigger compact />
        <SearchTrigger scope={{ editions: [], works: [7], chapters: [] }} label="Искать в томе" />
        <Probe />
      </MemoryRouter>,
    );
    await userEvent.keyboard('/');
    expect(screen.getAllByRole('dialog')).toHaveLength(1);
    // Побеждает конкретная затравка страницы, а не пустая из шапки: Enter в
    // открывшейся панели обязан уйти в поиск по тому 7, а не по всему
    // корпусу.
    await userEvent.type(screen.getByRole('searchbox'), 'партия{Enter}');
    expect(screen.getByTestId('loc')).toHaveTextContent('works=7');
  });

  it('клавиша / внутри поля ввода панель не открывает', async () => {
    render(
      <MemoryRouter>
        <input aria-label="чужое поле" />
        <SearchTrigger compact />
      </MemoryRouter>,
    );
    await userEvent.click(screen.getByLabelText('чужое поле'));
    await userEvent.keyboard('/');
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  // Предупреждение из рецензии панели (задача 12, пункт 1): без toggleRef,
  // прокинутого в SearchPanel, повторный клик по уже открытой затравке
  // добирается до document-обработчика useDrawerChrome как «клик мимо» и
  // закрывает панель тем же нажатием, что должно быть простым переоткрытием.
  it('повторный клик по затравке не закрывает панель как клик мимо', async () => {
    render(
      <MemoryRouter>
        <SearchTrigger compact />
      </MemoryRouter>,
    );
    const button = screen.getByRole('button', { name: /поиск/i });
    await userEvent.click(button);
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    // useDrawerChrome слушает native mousedown на document — тот же элемент,
    // но не через onClick, воспроизводит именно тот путь, которым слушатель
    // отличает «своя кнопка» от «мимо».
    fireEvent.mouseDown(button);
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  // Предупреждение из рецензии панели (задача 12, пункт 2): фокус должен
  // возвращаться на затравку — иначе клавиатурный читатель после закрытия
  // теряет место на странице (мирроринг ChapterTocDrawer.close).
  it('после закрытия фокус возвращается на затравку', async () => {
    render(
      <MemoryRouter>
        <SearchTrigger compact />
      </MemoryRouter>,
    );
    const button = screen.getByRole('button', { name: /поиск/i });
    await userEvent.click(button);
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(button).toHaveFocus();
  });

  // Круг правок 1, находка 2: панель раньше монтировалась один раз и не
  // размонтировалась на закрытии, поэтому её useState(initialQuery) не
  // перечитывал новое значение пропа при повторном открытии — читатель видел
  // прежний запрос, хотя адрес страницы уже сменился в обход панели (переход
  // по обычной ссылке, а не через саму панель).
  it('после смены запроса снаружи повторное открытие показывает новое значение, а не старое', async () => {
    const { rerender } = render(
      <MemoryRouter>
        <SearchTrigger compact initialQuery="старый запрос" />
      </MemoryRouter>,
    );
    const button = screen.getByRole('button', { name: /поиск/i });
    await userEvent.click(button);
    expect(screen.getByRole('searchbox')).toHaveValue('старый запрос');
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();

    // Смена пропа имитирует переход на другой /search?q=... в обход панели
    // (например, по ссылке «Искать в томе»): SearchTrigger остаётся
    // смонтированным, меняется только initialQuery.
    rerender(
      <MemoryRouter>
        <SearchTrigger compact initialQuery="новый запрос" />
      </MemoryRouter>,
    );
    await userEvent.click(screen.getByRole('button', { name: /поиск/i }));
    expect(screen.getByRole('searchbox')).toHaveValue('новый запрос');
  });
});
