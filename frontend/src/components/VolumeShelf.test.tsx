import { describe, it, expect } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { VolumeShelf, PEEK_DELAY_MS } from './VolumeShelf';
import { CARD_GAP } from '../utils/volumeCardPosition';
import type { VolumeSummary } from '../types';

/**
 * Подменяет прямоугольник книги: jsdom раскладку не считает и отдаёт нули.
 * Возвращает сдвижку — ею тест изображает уехавшую под указателем страницу.
 */
function stubRect(el: HTMLElement, top: number, left = 100): (nextTop: number) => void {
  let y = top;
  el.getBoundingClientRect = () =>
    ({
      top: y,
      bottom: y + 212,
      left,
      right: left + 64,
      width: 64,
      height: 212,
      x: left,
      y,
      toJSON: () => ({}),
    }) as DOMRect;
  return (nextTop: number) => {
    y = nextTop;
  };
}

function volumes(count: number): VolumeSummary[] {
  return Array.from({ length: count }, (_, i) => ({
    id: i + 1,
    title: `Том ${i + 1}`,
    author: '',
    language: '',
    country: '',
    file_path: '',
    status: 'draft',
    page_offset: 0,
    owner_id: 1,
    created_at: '',
    updated_at: '',
    volume_number: i + 1,
    pages_total: 100,
    pages_by_status: { вычитана: 25, вычитано_машиной: 25 },
    chapters_total: 1,
    top_chapters: [
      { title: `ГЛАВНАЯ РАБОТА ТОМА ${i + 1}`, pages: 80, share: 0.8 },
      { title: `ВТОРАЯ РАБОТА ТОМА ${i + 1}`, pages: 20, share: 0.2 },
    ],
  })) as VolumeSummary[];
}

function shelf(props: Partial<React.ComponentProps<typeof VolumeShelf>> = {}) {
  return render(
    <MemoryRouter>
      <VolumeShelf volumes={volumes(20)} {...props} />
    </MemoryRouter>,
  );
}

describe('VolumeShelf', () => {
  it('показывает все книги собрания', () => {
    const { container } = shelf();
    expect(container.querySelectorAll('.volume-slab')).toHaveLength(20);
    // Плитка «ещё N →» ушла вместе с обрезкой: полка показывает собрание
    // целиком, и обрезать больше нечего.
    expect(screen.queryByText(/ещё/)).toBeNull();
  });

  // Ради этого штабель и переделан: отсутствующий том виден как
  // отсутствующий, а не как сплошной ряд, в котором номера перескакивают.
  it('оставляет пустое место там, где тома в читальне нет', () => {
    const present = volumes(4).filter((v) => v.volume_number !== 3);
    const { container } = render(
      <MemoryRouter>
        <VolumeShelf volumes={present} />
      </MemoryRouter>,
    );
    expect(container.querySelectorAll('.volume-slab')).toHaveLength(3);
    expect(container.querySelectorAll('.shelf-run')).toHaveLength(1);
  });

  it('в сплошном ряду пустых мест не заводит', () => {
    const { container } = shelf();
    expect(container.querySelectorAll('.shelf-run')).toHaveLength(0);
  });

  // Длинный прогон пустых мест — главный источник серых плит на главной: у
  // Чернышевского четыре тома на пятнадцать мест, у Плеханова четыре на
  // двадцать четыре. Плита ничего не сообщает; свёрнутая в полосу, она
  // называет диапазон словами.
  it('длинный прогон пустых мест называет словами', () => {
    const present = volumes(20).filter((v) => v.volume_number! <= 2 || v.volume_number! >= 9);
    const { container } = render(
      <MemoryRouter>
        <VolumeShelf volumes={present} />
      </MemoryRouter>,
    );
    const run = container.querySelector('.shelf-run')!;
    expect(run).not.toBeNull();
    expect(run.textContent).toBe('Тома 3—8 ещё не сняты');
  });

  // Полоса говорит о содержимом читальни, а не о вёрстке, — и она
  // единственное место, где это сказано: счётчик собрания даёт итог «4 из
  // 15», но не называет, каких именно томов нет.
  it('свёрнутая полоса читается с экрана', () => {
    const present = volumes(20).filter((v) => v.volume_number! <= 2 || v.volume_number! >= 9);
    const { container } = render(
      <MemoryRouter>
        <VolumeShelf volumes={present} />
      </MemoryRouter>,
    );
    expect(container.querySelector('.shelf-run')).not.toHaveAttribute('aria-hidden');
  });

  it('прогон в один том называет том, а не диапазон', () => {
    const present = volumes(20).filter((v) => v.volume_number !== 6);
    const { container } = render(
      <MemoryRouter>
        <VolumeShelf volumes={present} />
      </MemoryRouter>,
    );
    expect(container.querySelector('.shelf-run')?.textContent).toBe('Том 6 ещё не снят');
  });

  // Прежде пустое место было немой полосой задника и от чтения с экрана
  // пряталось. Теперь оно говорит, каких томов нет, — и прятать эту фразу
  // нельзя: подпись собрания даёт итог «3 из 4», но не называет пропущенных.
  it('пустое место читается с экрана', () => {
    const present = volumes(4).filter((v) => v.volume_number !== 3);
    const { container } = render(
      <MemoryRouter>
        <VolumeShelf volumes={present} />
      </MemoryRouter>,
    );
    const run = container.querySelector('.shelf-run')!;
    expect(run).not.toHaveAttribute('aria-hidden');
    expect(run.textContent).toBe('Том 3 ещё не снят');
  });

  it('кладёт все книги одной мерки, каким бы ни был объём тома', () => {
    // Высота торца живёт в CSS (--slab-height) и одна на все книги. Толщина
    // по объёму тома здесь была дважды и оба раза снята: у стоящего корешка
    // она отнимала у подписи колонки набора, у лежащей книги не отнимала
    // ничего, но рваный штабель читается хуже ровного. Сторожим по
    // инлайновому стилю — вернуть толщину можно только им.
    const [thin, thick] = volumes(2);
    thin.pages_total = 331;
    thick.pages_total = 1097;
    const { container } = render(
      <MemoryRouter>
        <VolumeShelf volumes={[thin, thick]} />
      </MemoryRouter>,
    );
    const slabs = container.querySelectorAll<HTMLElement>('.volume-slab');
    expect(slabs).toHaveLength(2);
    for (const slab of slabs) {
      expect(slab.getAttribute('style')).toBeNull();
    }
  });

  it('помечает том, который читают сейчас', () => {
    const { container } = shelf({ currentWorkId: 3 });
    expect(container.querySelectorAll('.volume-slab.is-current')).toHaveLength(1);
  });

  it('до наведения карточки нет', () => {
    const { container } = shelf();
    expect(container.querySelector('.volume-card')).toBeNull();
  });

  it('на наведение показывает карточку с составом тома', async () => {
    const { container } = shelf();
    await userEvent.hover(screen.getByRole('link', { name: /^Том 2\./ }));

    await waitFor(() => expect(container.querySelector('.volume-card')).not.toBeNull());
    const card = container.querySelector('.volume-card')!;
    expect(card).toHaveAttribute('role', 'tooltip');
    expect(card.textContent).toContain('Том 2');
    expect(card.textContent).toContain('ГЛАВНАЯ РАБОТА ТОМА 2');
    expect(card.textContent).toContain('ВТОРАЯ РАБОТА ТОМА 2');
    expect(card.textContent).toContain('100 страниц · 1 работа');
    // Доля вычитки из карточки убрана вместе с остальными её следами.
    expect(card.textContent).not.toContain('вычитано');
  });

  // Одна карточка на всю полку, а не сорок пять: у корешка стоит
  // overflow: hidden, и карточка-потомок была бы им обрезана.
  it('карточка на полке всегда одна', async () => {
    const { container } = shelf();
    await userEvent.hover(screen.getByRole('link', { name: /^Том 2\./ }));
    await waitFor(() => expect(container.querySelector('.volume-card')).not.toBeNull());

    await userEvent.hover(screen.getByRole('link', { name: /^Том 5\./ }));
    await waitFor(() =>
      expect(container.querySelector('.volume-card')!.textContent).toContain('Том 5'),
    );
    expect(container.querySelectorAll('.volume-card')).toHaveLength(1);
  });

  it('связывает карточку с корешком, который она описывает', async () => {
    const { container } = shelf();
    const link = screen.getByRole('link', { name: /^Том 2\./ });
    await userEvent.hover(link);

    await waitFor(() => expect(container.querySelector('.volume-card')).not.toBeNull());
    const id = container.querySelector('.volume-card')!.getAttribute('id');
    expect(id).toBeTruthy();
    expect(link).toHaveAttribute('aria-describedby', id!);
    // Соседний корешок карточка не описывает.
    expect(screen.getByRole('link', { name: /^Том 5\./ })).not.toHaveAttribute('aria-describedby');
  });

  it('на уход указателя карточка исчезает', async () => {
    const { container } = shelf();
    const link = screen.getByRole('link', { name: /^Том 2\./ });
    await userEvent.hover(link);
    await waitFor(() => expect(container.querySelector('.volume-card')).not.toBeNull());

    await userEvent.unhover(link);
    await waitFor(() => expect(container.querySelector('.volume-card')).toBeNull());
  });

  // Карточка доступна с табуляции, а не только мышью.
  it('фокус на корешке тоже показывает карточку', async () => {
    const { container } = shelf();
    screen.getByRole('link', { name: /^Том 2\./ }).focus();
    await waitFor(() => expect(container.querySelector('.volume-card')).not.toBeNull());
  });

  // Корешок высокий: колесо мыши прокручивает страницу, а указатель остаётся
  // на нём же — mouseleave не приходит. Карточка стоит position: fixed по
  // координатам окна, поэтому снятый один раз прямоугольник устаревает ровно
  // на величину прокрутки, и карточка отрывается от корешка.
  it('на прокрутке карточка держится за корешок', async () => {
    const { container } = shelf();
    const link = screen.getByRole('link', { name: /^Том 2\./ });
    // Окно jsdom высотой 768: и до прокрутки, и после карточка помещается
    // под корешком, то есть сторону расчёт не меняет — меняется только
    // прямоугольник.
    const scrollTo = stubRect(link, 200);

    await userEvent.hover(link);
    await waitFor(() => expect(container.querySelector('.volume-card')).not.toBeNull());
    const card = () => container.querySelector<HTMLElement>('.volume-card')!;
    expect(card().style.top).toBe(`${200 + 212 + CARD_GAP}px`);

    scrollTo(100);
    // Карточка рисуется в коммите, а слушатель прокрутки вешается пассивным
    // эффектом позже: событие, посланное в этот зазор, пропадает. Поэтому
    // посылаем его заново на каждом опросе, пока слушатель не поймает.
    await waitFor(() => {
      window.dispatchEvent(new Event('scroll'));
      expect(card().style.top).toBe(`${100 + 212 + CARD_GAP}px`);
    });
  });

  // Между наведением и показом проходит задержка; страница за это время тоже
  // может уехать. Позиция обязана считаться от живого прямоугольника.
  it('прямоугольник берётся в момент показа, а не наведения', async () => {
    const { container } = shelf();
    const link = screen.getByRole('link', { name: /^Том 2\./ });
    const scrollTo = stubRect(link, 300);

    await userEvent.hover(link);
    scrollTo(100);

    await waitFor(() => expect(container.querySelector('.volume-card')).not.toBeNull());
    expect(container.querySelector<HTMLElement>('.volume-card')!.style.top).toBe(
      `${100 + 212 + CARD_GAP}px`,
    );
  });

  // У тома без страниц (том только заведён, страницы ещё не загружены) доли
  // вычитки нет — карточка обязана молчать про неё, а не писать «вычитано 0 %».
  it('у тома без страниц карточка молчит про вычитку', async () => {
    const empty: VolumeSummary = {
      ...volumes(1)[0],
      id: 99,
      volume_number: 99,
      pages_total: 0,
      pages_by_status: {},
      chapters_total: 0,
      top_chapters: [],
    };
    const { container } = render(
      <MemoryRouter>
        <VolumeShelf volumes={[...volumes(3), empty]} />
      </MemoryRouter>,
    );
    await userEvent.hover(screen.getByRole('link', { name: /^Том 99$/ }));

    await waitFor(() => expect(container.querySelector('.volume-card')).not.toBeNull());
    expect(container.querySelector('.volume-card')!.textContent).not.toContain('вычитано');
  });

  // Карточка — механизм наведения, а у пальца наведения нет. Браузер шлёт за
  // тапом эмулированные mouseover/mouseenter, поэтому без разбора указателя
  // карточка успевала мигнуть под уже уезжающей страницей. На тач-экране её
  // нет вовсе: том называет сам корешок, которому на узком экране вернули
  // подпись.
  it('после касания пальцем карточки нет', async () => {
    const { container } = shelf();
    const link = screen.getByRole('link', { name: /^Том 2\./ });

    fireEvent.pointerDown(link, { pointerType: 'touch' });
    fireEvent.mouseEnter(link);

    // Ждём дольше задержки показа: утверждение отрицательное, и проверять его
    // раньше срока значит проверять, что карточка ещё не успела появиться.
    await new Promise((resolve) => setTimeout(resolve, PEEK_DELAY_MS * 2));
    expect(container.querySelector('.volume-card')).toBeNull();
  });

  // Обратная сторона того же разбора: заглушить всё разом — не починка.
  it('после нажатия мышью карточка показывается', async () => {
    const { container } = shelf();
    const link = screen.getByRole('link', { name: /^Том 2\./ });

    fireEvent.pointerDown(link, { pointerType: 'mouse' });
    fireEvent.mouseEnter(link);

    await waitFor(() => expect(container.querySelector('.volume-card')).not.toBeNull());
  });

  // Жест, перехваченный браузером под прокрутку, эмулированных мышиных
  // событий за собой не ведёт — и вердикт пальца не должен пережить его и
  // достаться следующему, кто придёт с клавиатуры.
  it('отменённый жест не глушит следующий фокус', async () => {
    const { container } = shelf();
    const link = screen.getByRole('link', { name: /^Том 2\./ });

    fireEvent.pointerDown(link, { pointerType: 'touch' });
    fireEvent.pointerCancel(link, { pointerType: 'touch' });
    link.focus();

    await waitFor(() => expect(container.querySelector('.volume-card')).not.toBeNull());
  });
  // Узкий экран: сорок пять книг в один столбец растянули бы одно собрание на
  // полторы тысячи пикселей, а собраний пять. Прячет их медиазапрос, а не
  // условие в компоненте: ширину знает стиль, и второй источник правды о том
  // же пороге разошёлся бы с первым. Разметка лишь метит скрываемые места.
  describe('свёрнутый штабель', () => {
    it('метит книги за десятой', () => {
      const { container } = shelf();
      const marked = container.querySelectorAll('.shelf-stack > .is-overflow');
      expect(marked).toHaveLength(10);
      // Первые десять остаются видимыми при любой ширине.
      expect(container.querySelectorAll('.volume-slab:not(.is-overflow)')).toHaveLength(10);
    });

    it('держит штабель свёрнутым, пока не попросят иначе', () => {
      const { container } = shelf();
      expect(container.querySelector('.shelf-stack')).toHaveAttribute('data-collapsed');
      expect(screen.getByRole('button')).toHaveAttribute('aria-expanded', 'false');
    });

    it('кнопка называет, сколько томов в собрании', () => {
      shelf();
      expect(screen.getByRole('button')).toHaveTextContent('Показать все 20 томов');
    });

    // Тем же счётом, что подпись собрания (editionStats): у Маркса и Энгельса
    // подпись «50 из 50 томов», и кнопка «55 томов» под ней читалась бы
    // ошибкой. Книги-части одного тома — один том, книга без номера — ни одного.
    it('кнопка считает тома по номерам, а не книги', () => {
      const books = volumes(20);
      books[14] = { ...books[14], volume_number: 14, volume_part: 'II' };
      books[15] = { ...books[15], volume_number: undefined };
      shelf({ volumes: books });
      expect(screen.getByRole('button')).toHaveTextContent('Показать все 18 томов');
    });

    it('по нажатию раскрывает и складывает обратно', async () => {
      const { container } = shelf();
      const button = screen.getByRole('button');

      await userEvent.click(button);
      expect(container.querySelector('.shelf-stack')).not.toHaveAttribute('data-collapsed');
      expect(button).toHaveAttribute('aria-expanded', 'true');
      expect(button).toHaveTextContent('Свернуть');

      await userEvent.click(button);
      expect(container.querySelector('.shelf-stack')).toHaveAttribute('data-collapsed');
    });

    // Кнопка ради пары спрятанных томов — работа читателю без выгоды.
    it('короткого собрания не сворачивает', () => {
      const { container } = shelf({ volumes: volumes(8) });
      expect(screen.queryByRole('button')).toBeNull();
      expect(container.querySelector('.is-overflow')).toBeNull();
      expect(container.querySelector('.shelf-stack')).not.toHaveAttribute('data-collapsed');
    });
  });

  // Главная: пять собраний подряд, и два из них — сорок пять и пятьдесят
  // томов. На широком экране штабель идёт в два столбца, поэтому порог тут
  // свой — двенадцать книг, шесть на столбец, — и прячет их второй класс.
  // Первый остаётся за медиазапросом: одиннадцатая и двенадцатая книги
  // спрятаны на телефоне и показаны на мониторе, и одним классом это не
  // сказать.
  describe('свёрнутый и на широком экране', () => {
    it('метит книги за двенадцатой', () => {
      const { container } = shelf({ collapse: true });
      expect(container.querySelectorAll('.shelf-stack > .is-overflow-wide')).toHaveLength(8);
      expect(container.querySelectorAll('.volume-slab:not(.is-overflow-wide)')).toHaveLength(12);
      expect(container.querySelector('.shelf')).toHaveClass('shelf--compact');
    });

    // Страница собрания: сюда приходят именно за списком томов, и прятать их
    // тут значит заставлять жать кнопку каждый раз.
    it('без просьбы широкий экран показывает собрание целиком', () => {
      const { container } = shelf();
      expect(container.querySelector('.is-overflow-wide')).toBeNull();
      expect(container.querySelector('.shelf')).not.toHaveClass('shelf--compact');
    });

    // Два порога расходятся ровно на такой полке: тринадцати томов хватает,
    // чтобы свернуться на телефоне (десять показанных, три спрятанных), и не
    // хватает на мониторе (двенадцать показанных, один спрятанный). Признак
    // компактности поэтому вешается не по просьбе, а по тому, есть ли что
    // прятать: иначе на мониторе стояла бы кнопка, которая ничего не
    // раскрывает.
    it('не метит собрание, которому на широком экране прятать нечего', () => {
      const { container } = shelf({ collapse: true, volumes: volumes(13) });
      expect(container.querySelector('.is-overflow')).not.toBeNull();
      expect(container.querySelector('.is-overflow-wide')).toBeNull();
      expect(container.querySelector('.shelf')).not.toHaveClass('shelf--compact');
    });
  });
});
