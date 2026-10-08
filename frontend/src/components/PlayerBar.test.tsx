import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { usePlayer } from '../audio/playerStore';
import type { QueueItem } from '../audio/queue';
import { PlayerBar } from './PlayerBar';

function item(n: number): QueueItem {
  return {
    key: `track:${n}`,
    url: `/api/audio/${n}.opus`,
    downloadUrl: `/api/audio/${n}.opus?download=1`,
    title: `Товар. Часть ${n}`,
    subtitle: 'Капитал, т. 1 · синтез',
    href: `/works/4-kapital/chapters/${n}-tovar`,
    durationMs: 760_000,
  };
}
const Q = [item(1), item(2), item(3)];

const actions = {
  toggle: vi.fn(),
  seek: vi.fn(),
  next: vi.fn(),
  prev: vi.fn(),
  close: vi.fn(),
  retry: vi.fn(),
  setRate: vi.fn(),
};

function bar() {
  return render(
    <MemoryRouter>
      <PlayerBar />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  usePlayer.setState({
    queue: Q,
    index: 1,
    status: 'playing',
    position: 291,
    duration: 760,
    rate: 1,
    ...actions,
  });
});
afterEach(() => {
  vi.clearAllMocks();
  document.documentElement.style.removeProperty('--player-bar-height');
});

describe('PlayerBar', () => {
  it('пустая очередь — полосы нет', () => {
    usePlayer.setState({ queue: [], index: -1, status: 'idle' });
    const { container } = bar();
    expect(container).toBeEmptyDOMElement();
    expect(document.documentElement.style.getPropertyValue('--player-bar-height')).toBe('');
  });

  it('название ведёт к главе, время и перемотка, кнопки зовут действия', async () => {
    bar();
    expect(screen.getByRole('region', { name: 'Проигрыватель' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Товар. Часть 2' })).toHaveAttribute(
      'href',
      '/works/4-kapital/chapters/2-tovar',
    );
    expect(screen.getByText('Капитал, т. 1 · синтез')).toBeInTheDocument();
    const seek = screen.getByRole('slider', { name: 'Перемотка' });
    expect(seek).toHaveAttribute('aria-valuetext', '4 мин 51 с из 12 мин 40 с');
    fireEvent.change(seek, { target: { value: '300' } });
    expect(actions.seek).toHaveBeenCalledWith(300);

    await userEvent.click(screen.getByRole('button', { name: 'Пауза' }));
    await userEvent.click(screen.getByRole('button', { name: 'Предыдущая' }));
    await userEvent.click(screen.getByRole('button', { name: 'Следующая' }));
    await userEvent.click(screen.getByRole('button', { name: 'Закрыть проигрыватель' }));
    expect(actions.toggle).toHaveBeenCalledTimes(1);
    expect(actions.prev).toHaveBeenCalledTimes(1);
    expect(actions.next).toHaveBeenCalledTimes(1);
    expect(actions.close).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('link', { name: 'Скачать «Товар. Часть 2»' })).toHaveAttribute(
      'href',
      '/api/audio/2.opus?download=1',
    );
  });

  it('на паузе кнопка — «Слушать»; последняя дорожка — ⏭ неактивна', () => {
    usePlayer.setState({ index: 2, status: 'paused' });
    bar();
    expect(screen.getByRole('button', { name: 'Слушать' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Следующая' })).toBeDisabled();
  });

  it('до метаданных длительность — из элемента очереди', () => {
    usePlayer.setState({ status: 'loading', position: 0, duration: 0 });
    bar();
    expect(screen.getByRole('slider', { name: 'Перемотка' })).toHaveAttribute('max', '760');
  });

  it('ошибка — «Не удалось загрузить» и «Повторить»', async () => {
    usePlayer.setState({ status: 'error' });
    bar();
    expect(screen.getByRole('alert')).toHaveTextContent('Не удалось загрузить');
    await userEvent.click(screen.getByRole('button', { name: 'Повторить' }));
    expect(actions.retry).toHaveBeenCalledTimes(1);
  });

  it('пока видна — пишет свою высоту в --player-bar-height; закрылась — снимает', () => {
    const { rerender } = bar();
    expect(document.documentElement.style.getPropertyValue('--player-bar-height')).toBe('0px');
    usePlayer.setState({ queue: [], index: -1, status: 'idle' });
    rerender(
      <MemoryRouter>
        <PlayerBar />
      </MemoryRouter>,
    );
    expect(document.documentElement.style.getPropertyValue('--player-bar-height')).toBe('');
  });
});

// Рецензия ветки: выбор скорости, Esc, ✕ и «Повторить» убирают из DOM кнопку
// под фокусом — без переноса фокус падает на <body>, и следующий Tab
// начинает обход документа заново.
describe('фокус клавиатуры', () => {
  it('выбор скорости и Esc возвращают фокус на кнопку скорости', async () => {
    bar();
    const toggleRate = screen.getByRole('button', { name: 'Скорость 1×' });
    await userEvent.click(toggleRate);
    await userEvent.click(screen.getByRole('menuitemradio', { name: '1,5×' }));
    expect(toggleRate).toHaveFocus();
    await userEvent.click(toggleRate);
    await userEvent.keyboard('{Escape}');
    expect(toggleRate).toHaveFocus();
  });

  it('✕ уводит фокус в <main>', async () => {
    const main = document.createElement('main');
    main.id = 'main-content';
    main.tabIndex = -1;
    document.body.appendChild(main);
    try {
      bar();
      await userEvent.click(screen.getByRole('button', { name: 'Закрыть проигрыватель' }));
      expect(main).toHaveFocus();
    } finally {
      main.remove();
    }
  });

  it('«Повторить» оставляет фокус на кнопке игры', async () => {
    usePlayer.setState({ status: 'error' });
    bar();
    await userEvent.click(screen.getByRole('button', { name: 'Повторить' }));
    expect(screen.getByRole('button', { name: 'Слушать' })).toHaveFocus();
  });
});

describe('скорость', () => {
  it('кнопка открывает столбик, выбор ставит скорость и закрывает', async () => {
    bar();
    const toggleRate = screen.getByRole('button', { name: 'Скорость 1×' });
    expect(toggleRate).toHaveAttribute('aria-expanded', 'false');
    await userEvent.click(toggleRate);
    const items = screen.getAllByRole('menuitemradio');
    expect(items.map((b) => b.textContent)).toEqual([
      '2×',
      '1,75×',
      '1,5×',
      '1,25×',
      '1×',
      '0,75×',
    ]);
    expect(screen.getByRole('menuitemradio', { name: '1×' })).toHaveAttribute(
      'aria-checked',
      'true',
    );
    await userEvent.click(screen.getByRole('menuitemradio', { name: '1,5×' }));
    expect(actions.setRate).toHaveBeenCalledWith(1.5);
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('Esc и клик мимо закрывают столбик', async () => {
    bar();
    await userEvent.click(screen.getByRole('button', { name: 'Скорость 1×' }));
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('menu')).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: 'Скорость 1×' }));
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole('menu')).toBeNull();
    expect(actions.setRate).not.toHaveBeenCalled();
  });
});
