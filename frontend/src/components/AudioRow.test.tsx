import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { usePlayer } from '../audio/playerStore';
import { savePosition } from '../audio/positions';
import type { QueueItem } from '../audio/queue';
import { AudioRow } from './AudioRow';

function item(n: number): QueueItem {
  return {
    key: `track:${n}`,
    url: `/api/audio/${n}.opus`,
    downloadUrl: `/api/audio/${n}.opus?download=1`,
    title: `Часть ${n}`,
    subtitle: 'Капитал, т. 1 · синтез',
    href: '/works/4/chapters/1',
    durationMs: 760_000,
  };
}
const Q = [item(1), item(2)];

function row(key = 'track:2', queue = Q) {
  return render(
    <ul>
      <AudioRow
        itemKey={key}
        queue={queue}
        label="Товар. Часть вторая"
        playLabel="Товар. Часть вторая"
        durationMs={760_000}
        downloadUrl="/api/audio/2.opus?download=1"
        downloadLabel="Скачать «Товар. Часть вторая»"
      >
        <p>вместе с «Деньги»</p>
      </AudioRow>
    </ul>,
  );
}

const playQueue = vi.fn();
const toggle = vi.fn();

beforeEach(() => {
  localStorage.clear();
  usePlayer.setState({
    queue: [],
    index: -1,
    status: 'idle',
    position: 0,
    duration: 0,
    playQueue,
    toggle,
  });
});
afterEach(() => vi.clearAllMocks());

describe('AudioRow', () => {
  it('▶ ставит очередь с индексом этой строки; стрелка — ссылка скачивания', async () => {
    row();
    expect(screen.getByText('12 мин 40 с')).toBeInTheDocument();
    expect(screen.getByText('вместе с «Деньги»')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Скачать «Товар. Часть вторая»' })).toHaveAttribute(
      'href',
      '/api/audio/2.opus?download=1',
    );
    await userEvent.click(screen.getByRole('button', { name: 'Слушать: Товар. Часть вторая' }));
    expect(playQueue).toHaveBeenCalledWith(Q, 1);
    expect(toggle).not.toHaveBeenCalled();
  });

  it('играющая строка: ⏸ зовёт toggle, прогресс и «4:51 / 12:40»', async () => {
    usePlayer.setState({ queue: Q, index: 1, status: 'playing', position: 291, duration: 760 });
    const { container } = row();
    expect(container.querySelector('li')).toHaveClass('audio-row--current');
    expect(screen.getByText('4:51 / 12:40')).toBeInTheDocument();
    expect(container.querySelector('.audio-row-progress span')).toHaveStyle({
      width: `${(291 / 760) * 100}%`,
    });
    await userEvent.click(screen.getByRole('button', { name: 'Пауза: Товар. Часть вторая' }));
    expect(toggle).toHaveBeenCalledTimes(1);
    expect(playQueue).not.toHaveBeenCalled();
  });

  it('позиция растёт — строка перерисовывается', () => {
    usePlayer.setState({ queue: Q, index: 1, status: 'playing', position: 10, duration: 760 });
    row();
    act(() => usePlayer.setState({ position: 291 }));
    expect(screen.getByText('4:51 / 12:40')).toBeInTheDocument();
  });

  it('недослушанная, но не играющая — прогресс из памяти', () => {
    savePosition('track:2', 291, 760);
    usePlayer.setState({ queue: Q, index: 0, status: 'playing', position: 5, duration: 760 });
    const { container } = row();
    expect(container.querySelector('li')).not.toHaveClass('audio-row--current');
    expect(screen.getByText('4:51 / 12:40')).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: 'Слушать: Товар. Часть вторая' }),
    ).toBeInTheDocument();
  });

  it('строки нет в очереди (формат не играется) — без ▶, со стрелкой', () => {
    row('track:2', []);
    expect(screen.queryByRole('button')).toBeNull();
    expect(screen.getByRole('link', { name: 'Скачать «Товар. Часть вторая»' })).toBeInTheDocument();
  });
});
