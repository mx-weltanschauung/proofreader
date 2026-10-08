import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';
import { useReadingPrefs } from '../contexts/readingPrefsContext';
import { useToasterColors } from './useToasterColors';

// jsdom не считает каскад `[data-theme='...'] { --x: ... }` из index.css —
// contrast.test.ts на том же основании разбирает файл текстом, а не читает
// его через getComputedStyle. Чтобы честно смоделировать браузер (где
// getComputedStyle всегда отражает то, что применено прямо сейчас, а не
// какой-то более ранний снимок), подмена читает значение по текущему
// data-theme в момент вызова. Если хук спросит не вовремя — до того, как
// атрибут реально сменился, — получит цвет старой темы, как в настоящем
// браузере.
const BG_BY_THEME: Record<string, string> = { light: 'LIGHTBG', dark: 'DARKBG' };

function stubComputedStyleForTheme() {
  const real = window.getComputedStyle.bind(window);
  vi.spyOn(window, 'getComputedStyle').mockImplementation((el: Element, ...rest) => {
    if (el !== document.documentElement) return real(el, ...(rest as []));
    const theme = document.documentElement.getAttribute('data-theme') ?? 'light';
    return { getPropertyValue: () => BG_BY_THEME[theme] ?? '' } as unknown as CSSStyleDeclaration;
  });
}

function Probe() {
  const { setPref } = useReadingPrefs();
  const colors = useToasterColors();
  return (
    <div>
      <span data-testid="bg">{colors.cardBg}</span>
      <button onClick={() => setPref('theme', 'dark')}>переключить</button>
    </div>
  );
}

describe('useToasterColors', () => {
  beforeEach(() => {
    stubComputedStyleForTheme();
    document.documentElement.removeAttribute('data-theme');
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // Воспроизводит гонку из ревью: data-theme проставляет passive-эффект
  // ReadingPreferencesContext в родителе, а эффекты потомков отрабатывают
  // раньше эффектов родителя. Компонент, читающий цвет на рендер (или на
  // подписку без реакции на сам атрибут), навсегда — до следующего
  // случайного повторного рендера — остаётся с цветом предыдущей темы.
  it('подхватывает цвета новой темы после переключения', async () => {
    render(
      <ReadingPreferencesProvider>
        <Probe />
      </ReadingPreferencesProvider>,
    );
    expect(screen.getByTestId('bg')).toHaveTextContent('LIGHTBG');

    await userEvent.click(screen.getByText('переключить'));

    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    await waitFor(() => expect(screen.getByTestId('bg')).toHaveTextContent('DARKBG'));
  });
});
