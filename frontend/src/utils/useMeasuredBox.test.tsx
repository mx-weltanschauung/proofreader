import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { useState } from 'react';
import { useMeasuredBox } from './useMeasuredBox';

function Probe() {
  const ref = useMeasuredBox<HTMLDivElement>('data-hint-box');
  return <div ref={ref} data-testid="box" />;
}

// FeatureHint примонтирован всегда, но сам пузырёк — условно (portal рисуется
// только при open=true, как <div ref={bubbleRef}> в FeatureHint.tsx). Узел
// подключается к ref не на первом рендере хука, а позже, по клику.
function ConditionalProbe() {
  const [open, setOpen] = useState(false);
  const ref = useMeasuredBox<HTMLDivElement>('data-hint-box');
  return (
    <div>
      <button type="button" onClick={() => setOpen(true)}>
        открыть
      </button>
      {open && <div ref={ref} data-testid="box" />}
    </div>
  );
}

// Тот же узел, но в другую сторону — исчезает по клику. Нужен для гонки:
// узел отключается раньше, чем разрешится document.fonts.ready.
function ToggleProbe() {
  const [visible, setVisible] = useState(true);
  const ref = useMeasuredBox<HTMLDivElement>('data-hint-box');
  return (
    <div>
      <button type="button" onClick={() => setVisible(false)}>
        скрыть
      </button>
      {visible && <div ref={ref} data-testid="box" />}
    </div>
  );
}

/**
 * jsdom не реализует document.fonts вовсе, поэтому оба обычных теста файла
 * идут по синхронной ветке attach() (`else { settle(); }`) — асинхронная
 * ветка через document.fonts.ready и охрана nodeRef.current === node внутри
 * неё непокрыты ничем, кроме чтения кода. Подставляем document.fonts
 * промисом, которым управляет сам тест, чтобы дойти до этой ветки честно.
 */
function installControlledFonts() {
  let resolveReady: () => void = () => {};
  const ready = new Promise<void>((resolve) => {
    resolveReady = resolve;
  });
  Object.defineProperty(document, 'fonts', {
    value: { ready },
    configurable: true,
  });
  return { ready, resolveReady: () => resolveReady() };
}

afterEach(() => {
  vi.restoreAllMocks();
  // Подставной document.fonts — своя точечная порча jsdom для одного теста,
  // не должна утечь в соседние (в jsdom его нет вовсе, поэтому просто delete).
  delete (document as unknown as { fonts?: unknown }).fonts;
});

describe('крюк замерщика', () => {
  it('кладёт прямоугольник и ширину окна в атрибут', () => {
    // jsdom отдаёт нулевые прямоугольники; подменяем на правдоподобные.
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue({
      left: 12,
      top: 40,
      width: 280,
      height: 74,
    } as DOMRect);

    render(<Probe />);

    // document.fonts в jsdom нет, поэтому хук меряет прямо в эффекте.
    expect(screen.getByTestId('box')).toHaveAttribute(
      'data-hint-box',
      `12,40,280,74,${window.innerWidth}`,
    );
  });

  it('меряет и узел, подключённый к ref не на первом рендере', () => {
    // Ровно случай FeatureHint: хук вызывается всегда, а измеряемый узел
    // появляется позже (open стал true). Эффект с зависимостью только от
    // имени атрибута отработал бы один раз, ещё до появления узла, и больше
    // никогда — атрибут остался бы пустым навсегда. Живым замером
    // (frontend/scripts/measure-feature-hint.mjs) это и обнаружилось: пузырёк
    // в дампе есть, а data-hint-box — нет.
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue({
      left: 5,
      top: 6,
      width: 7,
      height: 8,
    } as DOMRect);

    render(<ConditionalProbe />);
    expect(screen.queryByTestId('box')).not.toBeInTheDocument();

    fireEvent.click(screen.getByText('открыть'));

    expect(screen.getByTestId('box')).toHaveAttribute(
      'data-hint-box',
      `5,6,7,8,${window.innerWidth}`,
    );
  });

  it('меряет после разрешения document.fonts.ready, не раньше', async () => {
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue({
      left: 1,
      top: 2,
      width: 3,
      height: 4,
    } as DOMRect);
    const { ready, resolveReady } = installControlledFonts();

    render(<Probe />);

    // Промис ещё не разрешён — измерение отложено, атрибута нет.
    expect(screen.getByTestId('box')).not.toHaveAttribute('data-hint-box');

    await act(async () => {
      resolveReady();
      await ready;
    });

    expect(screen.getByTestId('box')).toHaveAttribute(
      'data-hint-box',
      `1,2,3,4,${window.innerWidth}`,
    );
  });

  it('не пишет атрибут на узле, отключившемся до разрешения document.fonts.ready', async () => {
    // Та самая гонка, ради которой в attach() стоит сверка
    // nodeRef.current === node: узел успевает отключиться, пока
    // document.fonts.ready ещё не разрешён. Без охраны settle() отработает
    // на уже отсоединённом узле как ни в чём не бывало.
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue({
      left: 9,
      top: 9,
      width: 9,
      height: 9,
    } as DOMRect);
    const { ready, resolveReady } = installControlledFonts();

    render(<ToggleProbe />);
    const node = screen.getByTestId('box');

    fireEvent.click(screen.getByText('скрыть'));
    expect(screen.queryByTestId('box')).not.toBeInTheDocument();

    await act(async () => {
      resolveReady();
      await ready;
    });

    expect(node).not.toHaveAttribute('data-hint-box');
  });
});
