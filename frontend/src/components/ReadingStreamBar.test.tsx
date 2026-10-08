import { describe, expect, it, beforeEach, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';

import { ReadingStreamBar } from './ReadingStreamBar';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';

// Прокрутка в jsdom имитируется точно так же, как в ScrollDock.test.tsx:
// scrollY — геттер, простым присваиванием его не сдвинуть.
function scrollTo(y: number) {
  act(() => {
    Object.defineProperty(window, 'scrollY', { value: y, writable: true, configurable: true });
    window.dispatchEvent(new Event('scroll'));
  });
}

beforeEach(() => {
  Object.defineProperty(window, 'scrollY', { value: 0, writable: true, configurable: true });
  // jsdom реализует requestAnimationFrame настоящим ~16-мс таймером, а не
  // синхронно: без замены класс не успеет смениться до проверки.
  window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
    cb(0);
    return 0;
  }) as typeof window.requestAnimationFrame;
  window.cancelAnimationFrame = vi.fn();
});

function renderBar(props: Partial<React.ComponentProps<typeof ReadingStreamBar>> = {}) {
  return render(
    <MemoryRouter>
      <ReadingPreferencesProvider>
        <ReadingStreamBar
          chapterTitle="Глава первая. Товар"
          progressPercent={42}
          backHref="/works/47"
          showPageNumbers={false}
          onTogglePageNumbers={() => {}}
          suggestHref={null}
          scanHref={null}
          contentRef={{ current: null }}
          visiblePage={null}
          {...props}
        />
      </ReadingPreferencesProvider>
    </MemoryRouter>,
  );
}

describe('ReadingStreamBar', () => {
  // Маркер полосы в потоке стал якорем самого потока, и прежний переход на
  // отдельный экран полосы (скан рядом с текстом) переехал в панель — как и
  // в панели чтения главы.
  it('ведёт на скан видимой полосы', () => {
    renderBar({ scanHref: '/works/47/pages/43' });
    expect(screen.getByRole('link', { name: /скан страницы/i })).toHaveAttribute(
      'href',
      '/works/47/pages/43',
    );
  });

  // Панель потока узкая: на телефоне в неё уже не влезали «Скан страницы» и
  // «Предложить исправление» рядом — надписи налезали друг на друга. Обе
  // ссылки носят по паре подписей, как кнопки панели главы; полное имя при
  // этом держит aria-label (скрытая display:none подпись в доступное имя не
  // входит).
  it('носит у ссылок пару подписей: полную и короткую', () => {
    renderBar({ scanHref: '/works/47/pages/43', suggestHref: '/works/47/pages/43/suggest' });

    const scan = screen.getByRole('link', { name: 'Скан страницы' });
    expect(scan.querySelector('.toolbar-label-full')).toHaveTextContent('Скан страницы');
    expect(scan.querySelector('.toolbar-label-short')).toHaveTextContent('Скан');

    const suggest = screen.getByRole('link', { name: 'Предложить исправление' });
    expect(suggest.querySelector('.toolbar-label-full')).toHaveTextContent(
      'Предложить исправление',
    );
    expect(suggest.querySelector('.toolbar-label-short')).toHaveTextContent('Исправить');
  });

  // Скан — не продолжение чтения, а сверка с оригиналом: уходя по ссылке в
  // том же окне, читатель терял место в потоке и возвращался кнопкой «назад».
  // Поэтому скан открывается отдельным окном, а стрелка у надписи говорит об
  // этом заранее — до нажатия.
  it('открывает скан отдельным окном', () => {
    renderBar({ scanHref: '/works/47/pages/43' });

    const scan = screen.getByRole('link', { name: 'Скан страницы' });
    expect(scan).toHaveAttribute('target', '_blank');
    expect(scan).toHaveAttribute('rel', expect.stringContaining('noopener'));
  });

  it('помечает ссылку на скан значком нового окна', () => {
    renderBar({ scanHref: '/works/47/pages/43' });

    const mark = screen
      .getByRole('link', { name: 'Скан страницы' })
      .querySelector('.toolbar-label-external');
    expect(mark).toHaveTextContent('↗');
    // Значок — украшение надписи, в доступное имя ссылки он входить не должен.
    expect(mark).toHaveAttribute('aria-hidden', 'true');
  });

  // Правка полосы — продолжение работы над тем же местом, её окно менять не
  // за чем: проверка держит различие, чтобы «в новом окне» не расползлось на
  // соседнюю ссылку панели.
  it('ссылку на правку оставляет в том же окне', () => {
    renderBar({ suggestHref: '/works/47/pages/43/suggest' });

    expect(screen.getByRole('link', { name: 'Предложить исправление' })).not.toHaveAttribute(
      'target',
    );
  });

  it('без известной полосы ссылки на скан нет', () => {
    renderBar({ scanHref: null });
    expect(screen.queryByRole('link', { name: /скан страницы/i })).not.toBeInTheDocument();
  });

  it('показывает бегущий заголовок главы', () => {
    renderBar();
    expect(screen.getByText('Глава первая. Товар')).toBeInTheDocument();
  });

  it('без известной главы заголовка нет, но панель есть', () => {
    renderBar({ chapterTitle: null });
    expect(screen.getByRole('link', { name: /Выйти из чтения/i })).toBeInTheDocument();
  });

  it('даёт выход из чтения по переданному адресу', () => {
    renderBar();
    expect(screen.getByRole('link', { name: /Выйти из чтения/i })).toHaveAttribute(
      'href',
      '/works/47',
    );
  });

  it('полоса прогресса отражает долю прочитанного', () => {
    const { container } = renderBar({ progressPercent: 42 });
    const bar = container.querySelector('.reading-stream-progress');
    expect(bar).toHaveStyle({ width: '42%' });
  });

  // Полоса показывает долю на глаз, а цифра — точно: «примерно четверть» и
  // «42 %» отвечают на разные вопросы читателя.
  it('называет долю прочитанного цифрой', () => {
    renderBar({ progressPercent: 42 });
    expect(screen.getByText('42 %')).toBeInTheDocument();
  });

  // Панель прячется при движении вниз и возвращается при движении вверх:
  // читателю она нужна, когда он от текста оторвался, а не пока читает.
  it('прячется при прокрутке вниз и возвращается при прокрутке вверх', () => {
    const { container } = renderBar();
    const bar = container.querySelector('.reading-stream-bar');
    expect(bar).not.toHaveClass('is-hidden');

    scrollTo(400);
    expect(bar).toHaveClass('is-hidden');

    scrollTo(200);
    expect(bar).not.toHaveClass('is-hidden');
  });

  // У самого верха панель видна всегда: прятать нечего, читатель ещё не ушёл
  // в текст.
  it('у верха страницы остаётся видимой', () => {
    const { container } = renderBar();
    scrollTo(10);
    expect(container.querySelector('.reading-stream-bar')).not.toHaveClass('is-hidden');
  });
});

// Точка входа в правку: ведёт на видимую сейчас страницу потока, а не на
// страницу входа в чтение — та же логика, что и в панели чтения главы.
describe('ReadingStreamBar: предложить исправление', () => {
  it('ведёт на правку видимой страницы, когда адрес известен', () => {
    renderBar({ suggestHref: '/works/47/pages/50/suggest' });

    expect(screen.getByRole('link', { name: /предложить исправление/i })).toHaveAttribute(
      'href',
      '/works/47/pages/50/suggest',
    );
  });

  it('не показывает ссылку, пока видимая страница не определена', () => {
    renderBar({ suggestHref: null });

    expect(screen.queryByRole('link', { name: /предложить исправление/i })).not.toBeInTheDocument();
  });
});

// Печатный номер нужен для ссылки на источник, но в сплошном чтении мешает,
// поэтому он под тумблером — как и при чтении главы.
describe('ReadingStreamBar: тумблер номеров страниц', () => {
  it('состояние тумблера видно вспомогательным технологиям', () => {
    renderBar({ showPageNumbers: false });
    expect(screen.getByRole('button', { name: 'Номера страниц' })).toHaveAttribute(
      'aria-pressed',
      'false',
    );
  });

  it('включённый тумблер объявлен нажатым', () => {
    renderBar({ showPageNumbers: true });
    expect(screen.getByRole('button', { name: 'Номера страниц' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });

  it('нажатие просит экран переключить номера', async () => {
    const onToggle = vi.fn();
    renderBar({ showPageNumbers: false, onTogglePageNumbers: onToggle });

    await userEvent.click(screen.getByRole('button', { name: 'Номера страниц' }));

    expect(onToggle).toHaveBeenCalledTimes(1);
  });
});
