import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { ReadingSettings } from './ReadingSettings';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';
import { PREFS_KEY } from '../contexts/readingPrefs';

const setup = async () => {
  const user = userEvent.setup();
  render(
    // Панель теперь содержит ссылку на /help — Link из react-router-dom
    // требует роутер даже в изолированном тесте компонента.
    <MemoryRouter>
      <ReadingPreferencesProvider>
        <ReadingSettings />
      </ReadingPreferencesProvider>
    </MemoryRouter>,
  );
  return user;
};

const openPanel = async (user: ReturnType<typeof userEvent.setup>) => {
  await user.click(screen.getByRole('button', { name: 'Настройки чтения' }));
  return screen.getByRole('dialog', { name: 'Как читать' });
};

describe('ReadingSettings', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
  });

  it('is closed until the toggle is pressed', async () => {
    await setup();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('opens as a labelled modal dialog', async () => {
    const user = await setup();
    const panel = await openPanel(user);
    expect(panel).toHaveAttribute('aria-modal', 'true');
  });

  it('reports its open state on the toggle', async () => {
    const user = await setup();
    const toggle = screen.getByRole('button', { name: 'Настройки чтения' });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await user.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
  });

  it('applies a theme choice to the document root', async () => {
    const user = await setup();
    await openPanel(user);
    await user.click(screen.getByRole('radio', { name: 'OLED' }));
    expect(document.documentElement.getAttribute('data-theme')).toBe('oled');
  });

  it('applies a typeface choice', async () => {
    const user = await setup();
    await openPanel(user);
    await user.click(screen.getByRole('radio', { name: 'Fira Sans' }));
    expect(document.documentElement.getAttribute('data-reading-font')).toBe('fira');
  });

  it('applies a measure choice', async () => {
    const user = await setup();
    await openPanel(user);
    await user.click(screen.getByRole('radio', { name: '75 знаков' }));
    expect(document.documentElement.getAttribute('data-reading-measure')).toBe('wide');
  });

  it('steps the font size up and down within bounds', async () => {
    const user = await setup();
    await openPanel(user);
    const bigger = screen.getByRole('button', { name: 'Увеличить кегль' });
    await user.click(bigger);
    expect(document.documentElement.style.getPropertyValue('--reading-size')).toBe('19px');
    await user.click(screen.getByRole('button', { name: 'Уменьшить кегль' }));
    expect(document.documentElement.style.getPropertyValue('--reading-size')).toBe('18px');
  });

  it('disables the increase button at the maximum size', async () => {
    localStorage.setItem('reading-prefs', JSON.stringify({ fontSize: 24 }));
    const user = await setup();
    await openPanel(user);
    expect(screen.getByRole('button', { name: 'Увеличить кегль' })).toBeDisabled();
  });

  // Номера полос — единственная настройка, которую не видно в атрибутах
  // documentElement: её читают экраны чтения через контекст. Наблюдаемый
  // след — запись в localStorage, по ней и проверяем.
  it('shows page numbers as the standing choice', async () => {
    const user = await setup();
    await openPanel(user);
    expect(screen.getByRole('radio', { name: 'Показывать' })).toHaveAttribute(
      'aria-checked',
      'true',
    );
  });

  it('stores the choice to hide page numbers', async () => {
    const user = await setup();
    await openPanel(user);
    await user.click(screen.getByRole('radio', { name: 'Скрывать' }));
    expect(JSON.parse(localStorage.getItem(PREFS_KEY) ?? '{}').pageNumbers).toBe(false);
  });

  it('resets every preference', async () => {
    const user = await setup();
    await openPanel(user);
    await user.click(screen.getByRole('radio', { name: 'По формату' }));
    await user.click(screen.getByRole('button', { name: 'Сбросить всё' }));
    expect(document.documentElement.getAttribute('data-reading-align')).toBe('left');
  });

  it('closes on Escape and returns focus to the toggle', async () => {
    const user = await setup();
    await openPanel(user);
    await user.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Настройки чтения' })).toHaveFocus();
  });

  it('closes on the close button', async () => {
    const user = await setup();
    await openPanel(user);
    await user.click(screen.getByRole('button', { name: 'Закрыть' }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('closes on a click outside the panel', async () => {
    const user = await setup();
    await openPanel(user);
    await user.click(document.body);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('stays open when clicking inside the panel', async () => {
    const user = await setup();
    const panel = await openPanel(user);
    await user.click(screen.getByText('Тема'));
    expect(panel).toBeInTheDocument();
  });

  it('moves focus into the panel when it opens', async () => {
    const user = await setup();
    const panel = await openPanel(user);
    expect(panel.contains(document.activeElement)).toBe(true);
  });

  // The drawer listens for keydown on `document` in the capture phase and
  // calls stopPropagation() on Escape (ReadingSettings.tsx). ChapterView
  // listens on `window` in the bubble phase for its own immersive-mode
  // Escape handler. Capture always runs before bubble, so the drawer's
  // stopPropagation() must stop the event from ever reaching a window
  // bubble-phase listener.
  it('stops Escape from reaching a bubble-phase window listener', async () => {
    const user = await setup();
    await openPanel(user);

    const windowKeydownSpy = vi.fn();
    window.addEventListener('keydown', windowKeydownSpy);

    await user.keyboard('{Escape}');

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(windowKeydownSpy).not.toHaveBeenCalled();

    window.removeEventListener('keydown', windowKeydownSpy);
  });

  // Tab is trapped inside the drawer while it is open (modal): it must wrap
  // from the last focusable control back to the first, and Shift+Tab must
  // wrap from the first back to the last.
  it('traps Tab focus, wrapping at both ends of the panel', async () => {
    const user = await setup();
    const panel = await openPanel(user);

    const focusable = panel.querySelectorAll<HTMLElement>(
      'button:not([disabled]), [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
    );
    const first = focusable[0];
    const last = focusable[focusable.length - 1];

    last.focus();
    expect(last).toHaveFocus();
    await user.keyboard('{Tab}');
    expect(first).toHaveFocus();

    await user.keyboard('{Shift>}{Tab}{/Shift}');
    expect(last).toHaveFocus();
  });
});
