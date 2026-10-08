import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ScrollDockProvider } from './ScrollDockProvider';
import { useScrollDock, useScrollDockAction } from './scrollDockContext';

function Register({ label }: { label: string | null }) {
  const onActivate = vi.fn();
  useScrollDockAction(label === null ? null : { label, onActivate });
  return null;
}

// Счётчик рендеров для регрессии на бесконечный цикл: компонент намеренно
// создаёт новый callback на каждый рендер.
let unstableCallbackRenderCount = 0;

function RegisterWithUnstableCallback({ label }: { label: string | null }) {
  unstableCallbackRenderCount++;
  const onActivate = () => {};
  useScrollDockAction(label === null ? null : { label, onActivate });
  return null;
}

function Show() {
  const { action } = useScrollDock();
  return <div data-testid="label">{action?.label ?? '—'}</div>;
}

describe('ScrollDockProvider', () => {
  it('без регистрации действия нет', () => {
    render(
      <ScrollDockProvider>
        <Show />
      </ScrollDockProvider>,
    );
    expect(screen.getByTestId('label')).toHaveTextContent('—');
  });

  it('зарегистрированное действие видно потребителю', () => {
    render(
      <ScrollDockProvider>
        <Register label="II. Манифест" />
        <Show />
      </ScrollDockProvider>,
    );
    expect(screen.getByTestId('label')).toHaveTextContent('II. Манифест');
  });

  it('снимает действие при размонтировании', () => {
    const { rerender } = render(
      <ScrollDockProvider>
        <Register label="II. Манифест" />
        <Show />
      </ScrollDockProvider>,
    );
    rerender(
      <ScrollDockProvider>
        <Show />
      </ScrollDockProvider>,
    );
    expect(screen.getByTestId('label')).toHaveTextContent('—');
  });

  it('обновляет действие при смене подглавы', () => {
    const { rerender } = render(
      <ScrollDockProvider>
        <Register label="II. Манифест" />
        <Show />
      </ScrollDockProvider>,
    );
    rerender(
      <ScrollDockProvider>
        <Register label="III. Литература" />
        <Show />
      </ScrollDockProvider>,
    );
    expect(screen.getByTestId('label')).toHaveTextContent('III. Литература');
  });

  // Регрессия: новый callback на каждый рендер. Если регистрация зависит от
  // самой функции, а не только от подписи, рендер порождает setAction, тот —
  // новый рендер, и так по кругу. С callback'ом в ref рендеров один-два.
  it('безопасен для компонентов с нестабильным callback', () => {
    unstableCallbackRenderCount = 0;
    render(
      <ScrollDockProvider>
        <RegisterWithUnstableCallback label="II. Манифест" />
        <Show />
      </ScrollDockProvider>,
    );
    expect(unstableCallbackRenderCount).toBeLessThan(5);
    expect(screen.getByTestId('label')).toHaveTextContent('II. Манифест');
  });
});
