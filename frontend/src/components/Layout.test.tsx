import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom';
import { setAudioElement, usePlayer } from '../audio/playerStore';
import { FakeAudio, asAudio } from '../test/fakeAudio';
import { Layout } from './Layout';

vi.mock('./Header', () => ({ Header: () => <header /> }));
vi.mock('./Footer', () => ({ Footer: () => <footer /> }));

describe('Layout', () => {
  beforeEach(() => {
    window.scrollTo = vi.fn();
    Object.defineProperty(window, 'innerHeight', { value: 800, configurable: true });
    Object.defineProperty(window, 'scrollY', { value: 0, writable: true, configurable: true });
    window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
      cb(0);
      return 0;
    }) as typeof window.requestAnimationFrame;
    window.cancelAnimationFrame = vi.fn();
    window.matchMedia = vi.fn().mockReturnValue({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    }) as unknown as typeof window.matchMedia;
  });

  // «Наверх» нужна не только в чтении: список страниц тома тоже длинный.
  it('даёт кнопку «Наверх» на любой странице', () => {
    render(
      <MemoryRouter>
        <Layout>
          <p>содержимое</p>
        </Layout>
      </MemoryRouter>,
    );
    act(() => {
      Object.defineProperty(window, 'scrollY', { value: 1300, writable: true, configurable: true });
      window.dispatchEvent(new Event('scroll'));
    });
    expect(screen.getByRole('button', { name: 'Наверх' })).toBeInTheDocument();
  });

  // Нажатие уносит страницу вверх, слушатель прокрутки гасит блок, и кнопка,
  // на которой стоял клавиатурный фокус, исчезает из DOM. Без переноса фокус
  // падает на <body>, и следующий Tab начинает обход документа заново, ничего
  // об этом не сообщив. Цель переноса — сам <main>: он есть на любой странице
  // и в режиме чтения тоже, в отличие от шапки сайта, которая там скрыта
  // насовсем (display: none).
  it('«Наверх» уводит фокус в начало страницы, а не на body', async () => {
    const { container } = render(
      <MemoryRouter>
        <Layout>
          <p>содержимое</p>
        </Layout>
      </MemoryRouter>,
    );
    act(() => {
      Object.defineProperty(window, 'scrollY', { value: 1300, writable: true, configurable: true });
      window.dispatchEvent(new Event('scroll'));
    });

    await userEvent.click(screen.getByRole('button', { name: 'Наверх' }));

    expect(container.querySelector('main')).toHaveFocus();
  });

  // Ради этого полоса и живёт в Layout: читатель ушёл из главы в оглавление —
  // звук идёт, полоса на месте, файл заново не грузится.
  it('переход по читальне не снимает полосу и не перезапускает звук', async () => {
    const audio = new FakeAudio();
    setAudioElement(asAudio(audio));
    const item = {
      key: 'track:1',
      url: '/api/audio/1.opus',
      downloadUrl: '/api/audio/1.opus?download=1',
      title: 'Товар. Часть первая',
      subtitle: 'Капитал, т. 1 · синтез',
      href: '/chapter',
      durationMs: 760_000,
    };
    render(
      <MemoryRouter initialEntries={['/chapter']}>
        <Layout>
          <Routes>
            <Route path="/chapter" element={<Link to="/toc">К оглавлению</Link>} />
            <Route path="/toc" element={<p>Оглавление тома</p>} />
          </Routes>
        </Layout>
      </MemoryRouter>,
    );
    act(() => usePlayer.getState().playQueue([item], 0));
    expect(screen.getByRole('region', { name: 'Проигрыватель' })).toBeInTheDocument();

    await userEvent.click(screen.getByRole('link', { name: 'К оглавлению' }));

    expect(screen.getByText('Оглавление тома')).toBeInTheDocument();
    expect(screen.getByRole('region', { name: 'Проигрыватель' })).toBeInTheDocument();
    expect(audio.play).toHaveBeenCalledTimes(1);
    expect(audio.src).toBe('/api/audio/1.opus');
    act(() => usePlayer.getState().close());
  });
});
