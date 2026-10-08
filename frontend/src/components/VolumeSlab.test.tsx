import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { VolumeSlab } from './VolumeSlab';
import type { VolumeSummary } from '../types';

function volume(over: Partial<VolumeSummary>): VolumeSummary {
  return {
    id: 3,
    title: 'К. Маркс и Ф. Энгельс. Сочинения. Том 3',
    author: '',
    language: '',
    country: '',
    file_path: '',
    status: 'draft',
    page_offset: 0,
    owner_id: 1,
    created_at: '',
    updated_at: '',
    volume_number: 3,
    pages_total: 586,
    pages_by_status: {},
    chapters_total: 3,
    ...over,
  } as VolumeSummary;
}

const IDEOLOGY = {
  top_chapters: [
    { title: 'Немецкая идеология', pages: 538, share: 538 / 586 },
    { title: 'Тезисы о Фейербахе', pages: 4, share: 4 / 586 },
  ],
};

describe('VolumeSlab', () => {
  function setup(over: Partial<VolumeSummary> = {}, props: Record<string, unknown> = {}) {
    return render(
      <MemoryRouter>
        <VolumeSlab volume={volume(over)} current={false} {...props} />
      </MemoryRouter>,
    );
  }

  it('ведёт на том и называет его целиком', () => {
    setup(IDEOLOGY);
    const link = screen.getByRole('link', { name: 'Том 3. Немецкая идеология' });
    expect(link).toHaveAttribute('href', '/works/3');
  });

  it('показывает заглавие вдоль корешка и номер тома в хвосте', () => {
    const { container } = setup(IDEOLOGY);
    expect(container.querySelector('.volume-slab-plate')?.textContent).toBe('3');
    expect(container.querySelector('.volume-slab-label')?.textContent).toBe('Немецкая идеология');
  });

  // Номер впереди, а не в хвосте, — и это перемена против прежнего корешка.
  // Прежде номер стоял справа, чтобы сорок пять цифр не сложились в графу
  // таблицы; при подписи в одну строку это было верно. Строк теперь две, и
  // номер справа повисал бы между ними, не принадлежа ни одной. Впереди он
  // читается тем, чем и является на переплёте многотомника, — клеймом тома, —
  // а от графы его отличает кегль: 24px против 15px у заглавия, и в таблицах
  // так не бывает. Порядок в DOM и есть порядок на экране, обход с клавиатуры
  // читает книгу так же, как глаз.
  it('номер идёт в разметке раньше заглавия', () => {
    const { container } = setup(IDEOLOGY);
    const slab = container.querySelector('.volume-slab')!;
    const plate = slab.querySelector('.volume-slab-plate')!;
    const label = slab.querySelector('.volume-slab-label')!;
    expect(plate.compareDocumentPosition(label) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  // Склеенная через « · » подпись разгонялась до 194 знаков и гасла посреди
  // первой работы. Порознь каждая работа получает свою строку.
  it('кладёт каждую работу в свою строку', () => {
    const { container } = setup({
      pages_total: 600,
      top_chapters: [
        { title: 'Положение рабочего класса в Англии', pages: 287, share: 287 / 600 },
        { title: 'Святое семейство', pages: 228, share: 228 / 600 },
      ],
    });
    const lines = [...container.querySelectorAll('.volume-slab-label')].map((n) => n.textContent);
    expect(lines).toEqual(['Положение рабочего класса в Англии', 'Святое семейство']);
  });

  it('том, названный одной работой, даёт одну строку', () => {
    const { container } = setup(IDEOLOGY);
    expect(container.querySelectorAll('.volume-slab-label')).toHaveLength(1);
  });

  // Сорок две подписи из девяноста пяти приезжают из содержания капсом.
  // Текст не трогаем — «Х СЪЕЗД РКП(б)» приведением ломается, — но пометить
  // капс надо: гасит его кегль, а кегль знает только стиль.
  it('помечает подпись, набранную капсом', () => {
    const { container } = setup({
      top_chapters: [{ title: 'ЧТО ДЕЛАТЬ?', pages: 500, share: 0.9 }],
    });
    expect(container.querySelector('.volume-slab-label')).toHaveAttribute('data-caps');
  });

  it('обычную подпись капсом не метит', () => {
    const { container } = setup(IDEOLOGY);
    expect(container.querySelector('.volume-slab-label')).not.toHaveAttribute('data-caps');
  });

  // Тома 25 и 26 у Маркса и Энгельса вышли в нескольких книгах. «26·III»
  // одной строкой при кегле клейма занимает 65.4px против 41.6px колонки —
  // номер вылезал влево и перечёркивался задней стенкой. Часть уходит вторым
  // ярусом под номер: так клеймо занимает 27.9px, как обычное «55».
  it('часть тома ставит вторым ярусом под номером', () => {
    const { container } = setup({ volume_number: 26, volume_part: 'III', ...IDEOLOGY });
    const plate = container.querySelector('.volume-slab-plate')!;
    expect(plate.querySelector('.volume-slab-plate-number')?.textContent).toBe('26');
    expect(plate.querySelector('.volume-slab-plate-part')?.textContent).toBe('III');
  });

  it('у тома без частей второго яруса нет', () => {
    const { container } = setup(IDEOLOGY);
    expect(container.querySelector('.volume-slab-plate-part')).toBeNull();
  });

  // Чтению с экрана номер достаётся одной строкой: ярусы — приём вёрстки, а
  // «Том 26 III» без разделителя прочлось бы двумя разными числами.
  it('чтению с экрана называет том с частью через разделитель', () => {
    setup({ volume_number: 26, volume_part: 'III', ...IDEOLOGY });
    expect(screen.getByRole('link')).toHaveAttribute(
      'aria-label',
      'Том 26·III. Немецкая идеология',
    );
  });

  // Заливка показывала долю вычитки. Читальне она не нужна: при нуле
  // вычитанного все книги выглядели одинаково пустыми.
  it('книга не наливается долей вычитки', () => {
    const { container } = setup(IDEOLOGY);
    expect(container.querySelector('.volume-slab-fill')).toBeNull();
  });

  it('помечает том, который читают сейчас', () => {
    const { container } = render(
      <MemoryRouter>
        <VolumeSlab volume={volume({})} current />
      </MemoryRouter>,
    );
    expect(container.querySelector('.volume-slab.is-current')).not.toBeNull();
  });

  // Высота торца общая для всех книг и живёт в CSS (--slab-height). Толщина
  // по объёму тома здесь была дважды и оба раза снята; вернуть её можно
  // только инлайновым стилем, готовой раскладки jsdom не считает — его и
  // сторожим.
  it('не меряет книгу объёмом тома', () => {
    const { container } = setup({ pages_total: 1097 });
    const slab = container.querySelector<HTMLElement>('.volume-slab')!;
    expect(slab.getAttribute('style')).toBeNull();
  });

  it('на наведение и уход зовёт onPeek', async () => {
    const onPeek = vi.fn();
    setup(IDEOLOGY, { onPeek });
    const link = screen.getByRole('link');

    await userEvent.hover(link);
    expect(onPeek).toHaveBeenCalledTimes(1);
    expect(onPeek.mock.calls[0][0]).toBe(link);

    await userEvent.unhover(link);
    expect(onPeek).toHaveBeenCalledTimes(2);
    expect(onPeek.mock.calls[1][0]).toBeNull();
  });

  it('связывается с карточкой через aria-describedby', () => {
    setup(IDEOLOGY, { describedBy: 'volume-card' });
    expect(screen.getByRole('link')).toHaveAttribute('aria-describedby', 'volume-card');
  });

  it('без карточки описания не обещает', () => {
    setup(IDEOLOGY);
    expect(screen.getByRole('link')).not.toHaveAttribute('aria-describedby');
  });

  // Заглавие длиннее строки обрывается на торце книги, и обрыв надо
  // показать: иначе оно выглядит не сокращённым, а поломанным. Влезло или нет
  // — вопрос пикселей, а не длины строки: ширина знака у Literata своя у
  // каждой буквы, и «влезло» знает только движок. Поэтому решает замер, а
  // jsdom его не считает — ширины здесь подставлены.
  describe('обрыв подписи', () => {
    // Обе ширины объявлены на Element.prototype и в jsdom всегда нули;
    // подменяем их своими на HTMLElement.prototype, а после снимаем подмену —
    // унаследованные снова становятся видны.
    async function withWidths(scrollWidth: number, clientWidth: number) {
      for (const [name, value] of [
        ['scrollWidth', scrollWidth],
        ['clientWidth', clientWidth],
      ] as const) {
        Object.defineProperty(HTMLElement.prototype, name, {
          configurable: true,
          get: () => value,
        });
      }
      try {
        const label = setup(IDEOLOGY).container.querySelector('.volume-slab-label')!;
        // Замер читается сразу, а пишется кадром позже: в штабеле сорок пять
        // книг, и чередование чтения с записью пересчитывало бы раскладку на
        // каждой.
        await waitFor(() => expect(label).toHaveAttribute('data-fit'));
        return label;
      } finally {
        Reflect.deleteProperty(HTMLElement.prototype, 'scrollWidth');
        Reflect.deleteProperty(HTMLElement.prototype, 'clientWidth');
      }
    }

    it('помечает заглавие, не поместившееся в торец', async () => {
      expect(await withWidths(144, 61)).toHaveAttribute('data-clipped');
    });

    it('поместившееся заглавие не помечает', async () => {
      expect(await withWidths(48, 61)).not.toHaveAttribute('data-clipped');
    });

    // Замер уезжает в дамп: `npm run measure:volume-shelf` читает его из
    // страницы, потому что --dump-dom геометрии не отдаёт.
    it('кладёт замер в атрибут', async () => {
      expect(await withWidths(144, 61)).toHaveAttribute('data-fit', '144,61');
    });
  });
});
