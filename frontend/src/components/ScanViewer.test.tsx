import React, { useState } from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ScanViewer } from './ScanViewer';

// Библиотека зума тянет свою разметку и обработчики мыши; здесь проверяется
// оболочка, а не она. Подменяем её простой картинкой с видимым zoomType.
vi.mock('react-inner-image-zoom', () => ({
  default: ({ src, zoomType }: { src: string; zoomType: string }) => (
    <img src={src} alt="скан" data-zoom-type={zoomType} />
  ),
}));

describe('ScanViewer', () => {
  it('увеличивает по клику, а не по наведению', () => {
    render(<ScanViewer src="/scan.png" alt="Страница 3" />);
    expect(screen.getByAltText('скан')).toHaveAttribute('data-zoom-type', 'click');
  });

  it('открывает и закрывает полный экран', async () => {
    render(<ScanViewer src="/scan.png" alt="Страница 3" />);

    await userEvent.click(screen.getByRole('button', { name: 'Во весь экран' }));
    const dialog = screen.getByRole('dialog', { name: 'Страница 3' });
    expect(dialog).toBeInTheDocument();

    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('клик по самому скану не закрывает оверлей', async () => {
    render(<ScanViewer src="/scan.png" alt="Страница 3" />);
    await userEvent.click(screen.getByRole('button', { name: 'Во весь экран' }));

    // alt пропа уникален для картинки в оверлее — у заглушки zoom-библиотеки
    // (замокана выше) alt всегда "скан".
    await userEvent.click(screen.getByAltText('Страница 3'));

    expect(screen.getByRole('dialog', { name: 'Страница 3' })).toBeInTheDocument();
  });

  // Кнопка исчезает вместе с оверлеем — фокус не должен упасть на body.
  it('возвращает фокус на кнопку после закрытия', async () => {
    render(<ScanViewer src="/scan.png" alt="Страница 3" />);
    const open = screen.getByRole('button', { name: 'Во весь экран' });

    await userEvent.click(open);
    const dialog = screen.getByRole('dialog', { name: 'Страница 3' });

    // Закрываем кликом по подложке, а не по Escape: подложка — нефокусируемый
    // <div>, и клик по ней в браузере сам снимает фокус с кнопки (уводит на
    // body) ещё до onClick. Только явный возврат фокуса в close() возвращает
    // его на кнопку — Escape для этой проверки не годится, он ничей фокус не
    // трогает, и тест прошёл бы даже без механизма возврата.
    await userEvent.click(dialog);

    expect(open).toHaveFocus();
  });

  // onOpenChange — то, чем PageView отключает листание стрелками, пока
  // открыт полноэкранный просмотр; если колбэк не долетает, стрелки будут
  // одновременно возить скан и менять страницу под ним.
  //
  // Число вызовов проверяется наравне с аргументом: потребитель сегодня —
  // идемпотентный setScanOpen, и задвоение уведомления он бы проглотил, а
  // toHaveBeenLastCalledWith к повторам безразличен.
  it('сообщает наружу об открытии и закрытии ровно по разу', async () => {
    const onOpenChange = vi.fn();
    render(<ScanViewer src="/scan.png" alt="Страница 3" onOpenChange={onOpenChange} />);

    await userEvent.click(screen.getByRole('button', { name: 'Во весь экран' }));
    expect(onOpenChange).toHaveBeenCalledTimes(1);
    expect(onOpenChange).toHaveBeenLastCalledWith(true);

    await userEvent.keyboard('{Escape}');
    expect(onOpenChange).toHaveBeenCalledTimes(2);
    expect(onOpenChange).toHaveBeenLastCalledWith(false);
  });

  it('сообщает о закрытии по клику по подложке ровно один раз', async () => {
    const onOpenChange = vi.fn();
    render(<ScanViewer src="/scan.png" alt="Страница 3" onOpenChange={onOpenChange} />);

    await userEvent.click(screen.getByRole('button', { name: 'Во весь экран' }));
    onOpenChange.mockClear();

    await userEvent.click(screen.getByRole('dialog', { name: 'Страница 3' }));

    expect(onOpenChange).toHaveBeenCalledTimes(1);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  // Если сам ScanViewer размонтируется, пока просмотр открыт (у соседней
  // страницы, например, не оказалось превью), close() не вызывается — без
  // этого родитель навсегда остался бы думать, что просмотр всё ещё открыт.
  it('сообщает о закрытии при размонтировании открытым', async () => {
    const onOpenChange = vi.fn();
    const { unmount } = render(
      <ScanViewer src="/scan.png" alt="Страница 3" onOpenChange={onOpenChange} />,
    );

    await userEvent.click(screen.getByRole('button', { name: 'Во весь экран' }));
    onOpenChange.mockClear();

    unmount();

    expect(onOpenChange).toHaveBeenCalledTimes(1);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it('не повторяет уведомление при размонтировании после закрытия', async () => {
    const onOpenChange = vi.fn();
    const { unmount } = render(
      <ScanViewer src="/scan.png" alt="Страница 3" onOpenChange={onOpenChange} />,
    );

    await userEvent.click(screen.getByRole('button', { name: 'Во весь экран' }));
    await userEvent.keyboard('{Escape}');
    onOpenChange.mockClear();

    unmount();

    expect(onOpenChange).not.toHaveBeenCalled();
  });

  // Родитель вправе передать колбэк, созданный заново на каждом рендере.
  // Смена его идентичности не должна ничего сообщать наружу: ложное
  // «закрыто» поверх открытого оверлея вернуло бы листание стрелками — ровно
  // тот баг, ради которого уведомление и заведено.
  it('молчит, когда родитель пересоздаёт колбэк при открытом оверлее', async () => {
    const onOpenChange = vi.fn();

    const Parent: React.FC = () => {
      const [tick, setTick] = useState(0);
      return (
        <>
          <button type="button" onClick={() => setTick(tick + 1)}>
            Перерисовать
          </button>
          {/* Немемоизированный колбэк: новая функция на каждый рендер. */}
          <ScanViewer src="/scan.png" alt="Страница 3" onOpenChange={(o) => onOpenChange(o)} />
        </>
      );
    };

    render(<Parent />);
    await userEvent.click(screen.getByRole('button', { name: 'Во весь экран' }));
    onOpenChange.mockClear();

    await userEvent.click(screen.getByRole('button', { name: 'Перерисовать' }));

    expect(onOpenChange).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog', { name: 'Страница 3' })).toBeInTheDocument();
  });
});
