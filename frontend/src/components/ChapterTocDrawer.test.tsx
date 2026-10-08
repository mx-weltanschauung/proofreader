import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { Chapter } from '../types';
import { ChapterTocDrawer } from './ChapterTocDrawer';

function chapter(
  id: number,
  title: string,
  start: number,
  end: number,
  children?: Chapter[],
): Chapter {
  return {
    id,
    work_id: 4,
    title,
    type: 'chapter',
    order_number: id,
    start_page: start,
    end_page: end,
    children,
  } as Chapter;
}

// Поддерево «Нищеты философии»: у второй подглавы есть своя — она должна быть
// видна сразу, без раскрытия.
const SUBTREE: Chapter[] = [
  chapter(281, 'ПРЕДИСЛОВИЕ', 69, 70),
  chapter(282, 'Глава первая. НАУЧНОЕ ОТКРЫТИЕ', 71, 127, [
    chapter(293, '§ 1. Противоположность', 71, 90),
  ]),
  chapter(283, 'Глава вторая. МЕТАФИЗИКА', 128, 185),
];

function setup(chapters: Chapter[] = SUBTREE, activeChapterId?: number | null) {
  const onJump = vi.fn();
  const user = userEvent.setup();
  const view = render(
    <MemoryRouter>
      <ChapterTocDrawer
        work={{ id: 4 }}
        chapters={chapters}
        onJump={onJump}
        hrefFor={(c) => `#chapter-page-${c.start_page}`}
        activeChapterId={activeChapterId}
      />
    </MemoryRouter>,
  );
  return { onJump, user, ...view };
}

const openDrawer = async (user: ReturnType<typeof userEvent.setup>) => {
  await user.click(screen.getByRole('button', { name: 'Оглавление главы' }));
  return screen.getByRole('dialog', { name: 'Оглавление главы' });
};

describe('ChapterTocDrawer', () => {
  it('не рендерит ничего, когда у главы нет подглав', () => {
    setup([]);
    expect(screen.queryByRole('button', { name: 'Оглавление главы' })).toBeNull();
  });

  it('закрыт, пока не нажата кнопка', () => {
    setup();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('показывает всё поддерево развёрнутым, включая внуков', async () => {
    const { user } = setup();
    await openDrawer(user);
    expect(screen.getByRole('link', { name: /^ПРЕДИСЛОВИЕ/ })).toBeInTheDocument();
    expect(
      screen.getByRole('link', { name: /^Глава первая\. НАУЧНОЕ ОТКРЫТИЕ/ }),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /^§ 1\. Противоположность/ })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /^Глава вторая\. МЕТАФИЗИКА/ })).toBeInTheDocument();
  });

  it('показывает диапазон страниц подглавы', async () => {
    const { user } = setup();
    await openDrawer(user);
    expect(screen.getByRole('link', { name: /^ПРЕДИСЛОВИЕ/ })).toHaveTextContent('69–70');
  });

  it('по нажатию на заголовок зовёт onJump и закрывается', async () => {
    const { user, onJump } = setup();
    await openDrawer(user);
    await user.click(screen.getByRole('link', { name: /^Глава первая\. НАУЧНОЕ ОТКРЫТИЕ/ }));
    expect(onJump).toHaveBeenCalledTimes(1);
    expect(onJump.mock.calls[0][0].id).toBe(282);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('даёт отдельную ссылку на страницу подглавы', async () => {
    const { user, onJump } = setup();
    await openDrawer(user);
    const link = screen.getByRole('link', { name: 'Открыть отдельно: ПРЕДИСЛОВИЕ' });
    expect(link).toHaveAttribute('href', '/works/4/chapters/281');
    await user.click(link);
    // Переход — дело роутера, прокрутка здесь ни при чём.
    expect(onJump).not.toHaveBeenCalled();
  });

  it('закрывается по Escape и возвращает фокус на кнопку', async () => {
    const { user } = setup();
    await openDrawer(user);
    await user.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Оглавление главы' })).toHaveFocus();
  });

  it('закрывается по клику мимо панели', async () => {
    const { user } = setup();
    await openDrawer(user);
    await user.click(document.body);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  // Список подглав может опустеть, пока шторка была открыта (например,
  // кнопкой «Назад» браузера сменился маршрут). Панель размонтируется через
  // ранний return, но без фикса useDrawerChrome остаётся подписан на
  // document с open=true и первый Escape достаётся ей, а не режиму чтения.
  it('не глотает Escape, когда список опустел при открытой шторке', async () => {
    const onJump = vi.fn();
    const user = userEvent.setup();
    const { rerender } = render(
      <MemoryRouter>
        <ChapterTocDrawer
          work={{ id: 4 }}
          chapters={SUBTREE}
          onJump={onJump}
          hrefFor={() => '#x'}
        />
      </MemoryRouter>,
    );
    await user.click(screen.getByRole('button', { name: 'Оглавление главы' }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();

    rerender(
      <MemoryRouter>
        <ChapterTocDrawer work={{ id: 4 }} chapters={[]} onJump={onJump} hrefFor={() => '#x'} />
      </MemoryRouter>,
    );
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();

    let bubbled = false;
    window.addEventListener(
      'keydown',
      () => {
        bubbled = true;
      },
      { once: true },
    );
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    expect(bubbled).toBe(true);
  });

  it('помечает активную подглаву как текущую', async () => {
    const { user } = setup(SUBTREE, 282);
    await openDrawer(user);

    const active = screen.getByRole('link', { name: /^Глава первая\. НАУЧНОЕ ОТКРЫТИЕ/ });
    expect(active).toHaveAttribute('aria-current', 'true');
    expect(active.closest('li')).toHaveClass('is-active');
    expect(screen.getByRole('link', { name: /^ПРЕДИСЛОВИЕ/ })).not.toHaveAttribute('aria-current');
  });

  // Строка — настоящая ссылка с якорем подглавы: читатель берёт её из
  // контекстного меню («копировать адрес ссылки»), а обычный клик по-прежнему
  // прыгает по уже отрисованному тексту, не заводя записи в истории.
  it('несёт в строке якорь на начало подглавы', async () => {
    const { user } = setup();
    await openDrawer(user);

    expect(screen.getByRole('link', { name: /^ПРЕДИСЛОВИЕ/ })).toHaveAttribute(
      'href',
      '#chapter-page-69',
    );
  });

  it('без активной подглавы не помечает ничего', async () => {
    const { user } = setup();
    const panel = await openDrawer(user);
    expect(panel.querySelector('[aria-current]')).toBeNull();
  });

  it('докручивает список до активной строки при открытии', async () => {
    const { user } = setup(SUBTREE, 283);
    const scrolled = vi.fn();
    // Метод вешается на прототип: строки ещё нет, панель отрисуется по клику.
    const original = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = scrolled;

    try {
      await openDrawer(user);
      expect(scrolled).toHaveBeenCalledTimes(1);
      expect(scrolled.mock.calls[0][0]).toMatchObject({ block: 'center' });
    } finally {
      Element.prototype.scrollIntoView = original;
    }
  });

  it('ставит фокус на активную строку, а не на первый элемент панели', async () => {
    const { user } = setup(SUBTREE, 283);
    await openDrawer(user);
    expect(screen.getByRole('link', { name: /^Глава вторая\. МЕТАФИЗИКА/ })).toHaveFocus();
  });

  it('без активной подглавы фокус остаётся на первом элементе панели', async () => {
    const { user } = setup();
    await openDrawer(user);
    expect(screen.getByRole('button', { name: 'Закрыть' })).toHaveFocus();
  });

  // Проверка структурная, потому что отсечённый рендер снаружи ничем себя не
  // проявляет — а снять memo можно одним движением. Страница главы
  // перерисовывается на каждое изменение прогресса чтения и видимой страницы,
  // и без memo открытая шторка каждый раз реконсилила бы весь список подглав:
  // в томе 3 это сотни строк.
  it('обёрнута в memo, чтобы не перерисовываться вместе со страницей главы', () => {
    expect((ChapterTocDrawer as unknown as { $$typeof: symbol }).$$typeof).toBe(
      Symbol.for('react.memo'),
    );
  });
});
