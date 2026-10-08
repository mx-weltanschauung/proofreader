import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { VolumeDepthControl } from './VolumeDepthControl';
import { outlineMetrics } from '../utils/volumeOutline';
import { chapter } from '../test/chapterFixture';

const VOLUME_43 = [
  chapter(1875, 'Глава 1875', [
    chapter(1876, 'Глава 1876', [chapter(1885, 'Глава 1885')]),
    chapter(1881, 'Глава 1881'),
  ]),
];

// Шесть уровней — глубже SEGMENT_CAP (4), последний сегмент рисуется как
// «всё» вместо номера уровня.
const DEEP = [
  chapter(1, 'Глава 1', [
    chapter(2, 'Глава 2', [
      chapter(3, 'Глава 3', [
        chapter(4, 'Глава 4', [chapter(5, 'Глава 5', [chapter(6, 'Глава 6')])]),
      ]),
    ]),
  ]),
];

function setup(over: Partial<React.ComponentProps<typeof VolumeDepthControl>> = {}) {
  const user = userEvent.setup();
  const onPick = vi.fn();
  render(
    <VolumeDepthControl
      metrics={outlineMetrics(VOLUME_43)}
      depth={2}
      shown={3}
      total={4}
      disabled={false}
      onPick={onPick}
      {...over}
    />,
  );
  return { user, onPick };
}

describe('VolumeDepthControl', () => {
  it('показывает уровни тома и отмечает текущий', () => {
    setup();
    expect(screen.getByRole('button', { name: 'Глубина 2' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByRole('button', { name: 'Глубина 1' })).toHaveAttribute(
      'aria-pressed',
      'false',
    );
  });

  it('нажатие отдаёт выбранную глубину', async () => {
    const { user, onPick } = setup();
    await user.click(screen.getByRole('button', { name: 'Глубина 1' }));
    expect(onPick).toHaveBeenCalledWith(1);
  });

  it('пока видно не всё дерево, счётчик называет обе величины', () => {
    setup();
    expect(screen.getByText('3 из 4 строк')).toBeInTheDocument();
  });

  it('когда видно всё, счётчик называет одну величину', () => {
    setup({ shown: 4, total: 4 });
    expect(screen.getByText('4 строки')).toBeInTheDocument();
  });

  it('на томе без вложенности органа нет вовсе', () => {
    const { container } = render(
      <VolumeDepthControl
        metrics={outlineMetrics([chapter(1, 'Глава 1'), chapter(2, 'Глава 2')])}
        depth={1}
        shown={2}
        total={2}
        disabled={false}
        onPick={vi.fn()}
      />,
    );
    expect(container.querySelector('.vol-toc-depth')).toBeNull();
  });

  it('во время поиска сегменты недоступны, но счётчик остаётся', () => {
    setup({ disabled: true });
    expect(screen.getByRole('button', { name: 'Глубина 1' })).toBeDisabled();
    expect(screen.getByText('3 из 4 строк')).toBeInTheDocument();
  });

  /*
   * Проверка характеризующая, а не регрессионная: текст счётчика этой правкой
   * не менялся — «3 строки» рисовалось и раньше, потому что `shown < total`
   * при равных числах давало ту же вторую форму. Убрано само недостижимое
   * состояние, а не поведение. Здесь закреплено, что компонент без `total`
   * называет одну величину; порчу условия внутри компонента это поймает.
   */
  it('без общего числа счётчик называет одну величину', () => {
    setup({ shown: 3, total: undefined, disabled: true });
    expect(screen.getByText('3 строки')).toBeInTheDocument();
    expect(screen.queryByText(/из/)).toBeNull();
  });

  it('сегмент «всё» показывает этот текст и несёт его же в доступном имени', () => {
    setup({ metrics: outlineMetrics(DEEP), depth: 1, shown: 1, total: 6 });
    const button = screen.getByRole('button', { name: /всё/ });
    expect(button).toHaveTextContent('всё');
    // WCAG 2.5.3: доступное имя должно содержать видимый текст — «Глубина 6»
    // его не содержит, а «Глубина всё» содержит.
    expect(button.getAttribute('aria-label')).toContain('всё');
  });
});
