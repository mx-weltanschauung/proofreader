import { useEffect } from 'react';
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, useLocation } from 'react-router-dom';
import type { Chapter, PageMapEntry, Work } from '../types';
import { VolumeOutline } from './VolumeOutline';

const WORK = { id: 43, page_offset: 0, numbering_style: 'arabic' } as Work;

function chapter(
  id: number,
  title: string,
  start: number,
  end: number,
  children?: Chapter[],
): Chapter {
  return {
    id,
    work_id: 43,
    title,
    start_page: start,
    end_page: end,
    ...(children ? { children } : {}),
  } as Chapter;
}

// Уровни 1, 2, 1 — глубина по умолчанию при пороге 12 упрётся в предел (3).
const WRAPPED: Chapter[] = [
  chapter(1875, 'Искусство и литература', 1, 4, [
    chapter(1876, 'Письма без адреса', 1, 2, [chapter(1885, 'Письмо первое', 1, 1)]),
    chapter(1881, 'Генрик Ибсен', 3, 4),
  ]),
];

const AUTHORED: Chapter[] = [
  chapter(30, 'К. Маркс. О Прудоне', 1, 2),
  chapter(40, 'Ф. Энгельс. Барин Тидман', 3, 3),
];

// В отличие от AUTHORED это дерево вложено, поэтому у него есть глубина
// больше одного уровня и орган глубины действительно рисует сегменты —
// нужно для проверки того, что смена глубины гасит фасет автора.
const AUTHORED_NESTED: Chapter[] = [
  chapter(30, 'К. Маркс. О Прудоне', 1, 2, [chapter(31, 'Глава первая', 1, 2)]),
  chapter(40, 'Ф. Энгельс. Барин Тидман', 3, 5, [chapter(41, 'Глава про барина', 3, 5)]),
];

// Автор стоит на вложенных узлах, а не на корневых: у корня («Разное») своего
// автора нет, различаются только дети. Воспроизводит слепоту фильтра фасета к
// дереву глубже одного уровня (находки 1 и 2 финального ревью) — в AUTHORED и
// AUTHORED_NESTED автор всегда есть на корне, и фильтрация по корню там
// «работает» случайно.
const AUTHORED_DEEP: Chapter[] = [
  chapter(60, 'Разное', 1, 5, [
    chapter(61, 'К. Маркс. О Прудоне', 1, 2),
    chapter(62, 'Ф. Энгельс. Барин Тидман', 3, 5),
  ]),
];

const PAGES: PageMapEntry[] = [1, 2, 3, 4, 5].map((page_number) => ({
  page_number,
  status: 'не_вычитана' as const,
}));

function setup(
  props: Partial<React.ComponentProps<typeof VolumeOutline>> = {},
  route = '/works/43',
) {
  const user = userEvent.setup();
  render(
    <MemoryRouter initialEntries={[route]}>
      <VolumeOutline
        work={WORK}
        chapters={WRAPPED}
        pages={PAGES}
        editable={false}
        highlight={null}
        {...props}
      />
    </MemoryRouter>,
  );
  return { user };
}

/** Складывает ключ каждой записи истории: push заводит новый, replace — нет. */
function LocationProbe({ onKey }: { onKey: (key: string) => void }) {
  const { key } = useLocation();
  useEffect(() => {
    onKey(key);
  }, [key, onKey]);
  return null;
}

describe('VolumeOutline', () => {
  it('открывается на глубине по умолчанию и показывает всё дерево тома', () => {
    setup();
    expect(screen.getByRole('link', { name: 'Искусство и литература' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Письма без адреса' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Письмо первое' })).toBeInTheDocument();
  });

  it('сегмент 1 оставляет только верхний уровень', async () => {
    const { user } = setup();
    await user.click(screen.getByRole('button', { name: 'Глубина 1' }));
    expect(screen.getByRole('link', { name: 'Искусство и литература' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Письма без адреса' })).toBeNull();
  });

  it('глубина из адреса перекрывает умолчание', () => {
    setup({}, '/works/43?depth=1');
    expect(screen.queryByRole('link', { name: 'Письма без адреса' })).toBeNull();
  });

  it('мусор в адресе молча откатывается на умолчание, без сообщения об ошибке', () => {
    setup({}, '/works/43?depth=жжж');
    expect(screen.getByRole('link', { name: 'Письмо первое' })).toBeInTheDocument();
  });

  it('треугольник раскрывает узел глубже базовой глубины', async () => {
    const { user } = setup({}, '/works/43?depth=1');
    await user.click(screen.getByRole('button', { name: 'Развернуть: Искусство и литература' }));
    expect(screen.getByRole('link', { name: 'Письма без адреса' })).toBeInTheDocument();
  });

  it('треугольник сворачивает узел внутри базовой глубины', async () => {
    const { user } = setup();
    await user.click(screen.getByRole('button', { name: 'Свернуть: Письма без адреса' }));
    expect(screen.queryByRole('link', { name: 'Письмо первое' })).toBeNull();
  });

  it('смена глубины гасит ручные раскрытия', async () => {
    const { user } = setup();
    await user.click(screen.getByRole('button', { name: 'Свернуть: Письма без адреса' }));
    await user.click(screen.getByRole('button', { name: 'Глубина 2' }));
    await user.click(screen.getByRole('button', { name: 'Глубина 3' }));
    expect(screen.getByRole('link', { name: 'Письмо первое' })).toBeInTheDocument();
  });

  it('нажатие на уже выбранный сегмент не кладёт шаг в историю', async () => {
    const keys: string[] = [];
    // Ссылка стабильная: инлайновая стрелка меняла бы список зависимостей
    // эффекта на каждый рендер, и зонд писал бы ключ впустую.
    const push = (key: string) => {
      keys.push(key);
    };
    const user = userEvent.setup();
    render(
      <MemoryRouter initialEntries={['/works/43']}>
        <VolumeOutline
          work={WORK}
          chapters={WRAPPED}
          pages={PAGES}
          editable={false}
          highlight={null}
        />
        <LocationProbe onKey={push} />
      </MemoryRouter>,
    );

    // В WRAPPED уровни 1, 2, 1: предел 3, умолчание тоже 3 — нажатый сегмент
    // это «Глубина 3».
    const before = keys.length;
    await user.click(screen.getByRole('button', { name: /Глубина/, pressed: true }));
    // «Назад» после такого нажатия отматывал бы пустой шаг, ничего не меняя
    // на экране.
    expect(keys).toHaveLength(before);
  });

  it('поиск показывает плоскую выдачу с путём', async () => {
    const { user } = setup();
    await user.type(screen.getByRole('searchbox', { name: 'Поиск по работам' }), 'письмо первое');
    expect(screen.getByRole('link', { name: 'Письмо первое' })).toBeInTheDocument();
    expect(screen.getByText('Искусство и литература › Письма без адреса')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Генрик Ибсен' })).toBeNull();
  });

  it('во время поиска орган глубины недоступен', async () => {
    const { user } = setup();
    await user.type(screen.getByRole('searchbox', { name: 'Поиск по работам' }), 'ибсен');
    expect(screen.getByRole('button', { name: 'Глубина 1' })).toBeDisabled();
  });

  it('погашенный поиск возвращает прежнее дерево с прежними раскрытиями', async () => {
    const { user } = setup();
    await user.click(screen.getByRole('button', { name: 'Свернуть: Письма без адреса' }));
    const box = screen.getByRole('searchbox', { name: 'Поиск по работам' });
    await user.type(box, 'ибсен');
    await user.clear(box);
    expect(screen.queryByRole('link', { name: 'Письмо первое' })).toBeNull();
  });

  it('объясняет пустую выдачу и даёт сбросить поиск', async () => {
    const { user } = setup();
    await user.type(screen.getByRole('searchbox', { name: 'Поиск по работам' }), 'жжж');
    expect(screen.getByText('По запросу «жжж» ничего нет')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Сбросить' }));
    expect(screen.getByRole('link', { name: 'Генрик Ибсен' })).toBeInTheDocument();
  });

  /*
   * Том со страницами, но без глав. Прежде показывалось «Работы не найдены» с
   * кнопкой «Сбросить», которой нечего сбрасывать: и запрос, и фасет уже
   * пусты. Поведение унаследовано от VolumeIndex.
   */
  it('том без глав объясняет это и не предлагает ничего сбрасывать', () => {
    setup({ chapters: [] });
    expect(screen.getByText('Том ещё не расписан по главам')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Сбросить' })).toBeNull();
  });

  it('фасет автора оставляет только его работы', async () => {
    const { user } = setup({ chapters: AUTHORED });
    await user.click(screen.getByRole('button', { name: 'Ф. Энгельс' }));
    expect(screen.queryByRole('link', { name: 'О Прудоне' })).toBeNull();
    expect(screen.getByRole('link', { name: 'Барин Тидман' })).toBeInTheDocument();
  });

  it('смена глубины гасит фасет автора', async () => {
    const { user } = setup({ chapters: AUTHORED_NESTED });
    const facet = screen.getByRole('button', { name: 'Ф. Энгельс' });

    await user.click(facet);
    expect(facet).toHaveAttribute('aria-pressed', 'true');
    expect(screen.queryByRole('link', { name: 'О Прудоне' })).toBeNull();

    await user.click(screen.getByRole('button', { name: 'Глубина 1' }));

    expect(screen.getByRole('button', { name: 'Ф. Энгельс' })).toHaveAttribute(
      'aria-pressed',
      'false',
    );
    expect(screen.getByRole('link', { name: 'О Прудоне' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Барин Тидман' })).toBeInTheDocument();
  });

  it('фасет автора на вложенном уровне оставляет узел с совпавшими потомками, а не весь корень целиком', async () => {
    const { user } = setup({ chapters: AUTHORED_DEEP });
    await user.click(screen.getByRole('button', { name: 'Ф. Энгельс' }));

    // Корень «Разное» своего автора не несёт, но у него есть совпавший
    // потомок — узел должен остаться, а не пропасть вместе со всем деревом.
    expect(screen.getByRole('link', { name: 'Разное' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Барин Тидман' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'О Прудоне' })).toBeNull();
    expect(screen.queryByText('Работы не найдены')).toBeNull();
  });

  it('фасет на вложенном уровне убирает целую ветку без совпадений', async () => {
    const { user } = setup({ chapters: AUTHORED_DEEP });
    await user.click(screen.getByRole('button', { name: 'К. Маркс' }));

    expect(screen.getByRole('link', { name: 'О Прудоне' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Барин Тидман' })).toBeNull();
  });

  it('счётчик строк на фасете вложенного уровня считает показанное, а не всё дерево', async () => {
    const { user } = setup({ chapters: AUTHORED_DEEP });
    // Полное дерево тома: корень + два ребёнка — 3 строки.
    expect(screen.getByText('3 строки')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Ф. Энгельс' }));

    // После фасета видно только корень и совпавшего ребёнка — 2 строки из 3.
    expect(screen.getByText('2 из 3 строк')).toBeInTheDocument();
    expect(screen.queryByText('3 строки')).toBeNull();
  });

  /*
   * Контракт задачи живёт здесь, а не в проверке компонента: тайпчек не мешает
   * снова передать в поиске число, и тогда счётчик стал бы говорить «N из N».
   * Ищем счётчик по форме текста, а не по точному числу: оно зависит от того,
   * сколько глав фикстуры совпало с запросом.
   */
  it('в поиске счётчик называет одну величину, без «из»', async () => {
    const { user } = setup();
    await user.type(screen.getByRole('searchbox', { name: 'Поиск по работам' }), 'Письмо');
    const counter = screen.getByText(/строк/);
    expect(counter).toHaveTextContent(/^\d+ строк/);
    expect(counter).not.toHaveTextContent(/из/);
  });

  it('колонки автора нет в томе, где авторов не пишут', () => {
    const { container } = render(
      <MemoryRouter>
        <VolumeOutline
          work={WORK}
          chapters={WRAPPED}
          pages={PAGES}
          editable={false}
          highlight={null}
        />
      </MemoryRouter>,
    );
    expect(container.querySelector('.vol-toc-author')).toBeNull();
  });

  it('блок вне оглавления виден при пустом запросе', () => {
    setup();
    expect(screen.getByText('Вне оглавления')).toBeInTheDocument();
    expect(screen.getByText(/5—5/)).toBeInTheDocument();
  });

  it('во время поиска блока вне оглавления нет', async () => {
    const { user } = setup();
    await user.type(screen.getByRole('searchbox', { name: 'Поиск по работам' }), 'ибсен');
    expect(screen.queryByText('Вне оглавления')).toBeNull();
  });
});
