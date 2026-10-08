import { describe, it, expect, vi } from 'vitest';
import { createElement } from 'react';
import { renderHook, fireEvent, screen } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { usePageKeyboardNav } from './usePageKeyboardNav';

// Пробник маршрута вместо мока useNavigate: так тест проходит через
// настоящий MemoryRouter и настоящий navigate(), а не только проверяет,
// что мок дёрнули с нужной строкой. Дороже писать, но честнее — ловит и
// опечатку в href, и обрыв интеграции с react-router.
function LocationProbe() {
  const location = useLocation();
  return createElement('span', { 'data-testid': 'location' }, location.pathname);
}

function renderNav(prevHref: string | null, nextHref: string | null, enabled = true) {
  return renderHook(() => usePageKeyboardNav(prevHref, nextHref, enabled), {
    wrapper: ({ children }) =>
      createElement(
        MemoryRouter,
        { initialEntries: ['/works/41/pages/3'] },
        createElement(LocationProbe),
        children,
      ),
  });
}

function currentPath(): string {
  return screen.getByTestId('location').textContent ?? '';
}

describe('usePageKeyboardNav', () => {
  it('ArrowLeft уводит на prevHref', () => {
    renderNav('/works/41/pages/2', '/works/41/pages/4');
    fireEvent.keyDown(window, { key: 'ArrowLeft' });
    expect(currentPath()).toBe('/works/41/pages/2');
  });

  it('ArrowRight уводит на nextHref', () => {
    renderNav('/works/41/pages/2', '/works/41/pages/4');
    fireEvent.keyDown(window, { key: 'ArrowRight' });
    expect(currentPath()).toBe('/works/41/pages/4');
  });

  it('на краю диапазона нажатие не листает никуда', () => {
    renderNav(null, '/works/41/pages/4');
    fireEvent.keyDown(window, { key: 'ArrowLeft' });
    expect(currentPath()).toBe('/works/41/pages/3');
  });

  it('не листает при фокусе в input', () => {
    renderNav('/works/41/pages/2', '/works/41/pages/4');
    const input = document.createElement('input');
    document.body.appendChild(input);
    input.focus();

    fireEvent.keyDown(input, { key: 'ArrowLeft' });

    expect(currentPath()).toBe('/works/41/pages/3');
    input.remove();
  });

  it('не листает при фокусе в textarea', () => {
    renderNav('/works/41/pages/2', '/works/41/pages/4');
    const textarea = document.createElement('textarea');
    document.body.appendChild(textarea);
    textarea.focus();

    fireEvent.keyDown(textarea, { key: 'ArrowRight' });

    expect(currentPath()).toBe('/works/41/pages/3');
    textarea.remove();
  });

  it('не листает при фокусе в select', () => {
    renderNav('/works/41/pages/2', '/works/41/pages/4');
    const select = document.createElement('select');
    document.body.appendChild(select);
    select.focus();

    fireEvent.keyDown(select, { key: 'ArrowLeft' });

    expect(currentPath()).toBe('/works/41/pages/3');
    select.remove();
  });

  it('не листает в редактируемом элементе (contenteditable)', () => {
    renderNav('/works/41/pages/2', '/works/41/pages/4');
    const editable = document.createElement('div');
    // jsdom не реализует геттер isContentEditable (вернёт undefined что при
    // атрибуте contenteditable="true", что без него) — задаём свойство
    // явно, чтобы тест шёл по той же ветке кода, что сработает в браузере,
    // а не полагался на побочный эффект отсутствия проверки.
    editable.setAttribute('contenteditable', 'true');
    Object.defineProperty(editable, 'isContentEditable', { value: true, configurable: true });
    document.body.appendChild(editable);
    editable.focus();

    fireEvent.keyDown(editable, { key: 'ArrowRight' });

    expect(currentPath()).toBe('/works/41/pages/3');
    editable.remove();
  });

  // Alt+← — это «назад» браузера; перехватывать его нельзя ни при каком
  // соседе. То же для Ctrl/Meta/Shift — модификатор всегда отменяет переход.
  it.each(['altKey', 'ctrlKey', 'metaKey', 'shiftKey'] as const)(
    'не листает при модификаторе %s',
    (modifier) => {
      renderNav('/works/41/pages/2', '/works/41/pages/4');

      fireEvent.keyDown(window, { key: 'ArrowLeft', [modifier]: true });

      expect(currentPath()).toBe('/works/41/pages/3');
    },
  );

  // Пока открыт полноэкранный просмотр скана, стрелками возят увеличенную
  // картинку в ScanViewer, а не листают страницы работы под ним.
  it('не листает, пока enabled=false', () => {
    renderNav('/works/41/pages/2', '/works/41/pages/4', false);

    fireEvent.keyDown(window, { key: 'ArrowLeft' });
    expect(currentPath()).toBe('/works/41/pages/3');

    fireEvent.keyDown(window, { key: 'ArrowRight' });
    expect(currentPath()).toBe('/works/41/pages/3');
  });

  it('снимает обработчик при размонтировании', () => {
    const removeSpy = vi.spyOn(window, 'removeEventListener');
    const { unmount } = renderNav('/works/41/pages/2', '/works/41/pages/4');

    unmount();

    expect(removeSpy).toHaveBeenCalledWith('keydown', expect.any(Function));
  });
});
