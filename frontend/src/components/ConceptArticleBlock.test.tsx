import { expect, it, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { ConceptArticleBlock } from './ConceptArticleBlock';
import type { ConceptArticle, ConceptReference } from '../types';

const article: ConceptArticle = {
  id: 1,
  edition_id: 1,
  edition_title: 'Маркс и Энгельс',
  work_id: 40,
  source_url: '',
  title: 'Абстрактный труд',
  kind: 'article',
  article_markdown: '',
  source_page_start: 11,
  source_page_end: 11,
  links: [],
  references: [
    {
      id: 1,
      article_id: 1,
      volume_number: 12,
      page_start: 730,
      page_end: 731,
      rubric: 'определение',
      order_number: 1,
      is_uncertain: false,
      resolved: false,
    },
    {
      id: 2,
      article_id: 1,
      volume_number: 13,
      page_start: 16,
      page_end: 18,
      rubric: 'его мера',
      order_number: 2,
      is_uncertain: false,
      resolved: false,
    },
    {
      id: 3,
      article_id: 1,
      volume_number: 13,
      page_start: 43,
      page_end: 43,
      rubric: 'определение',
      order_number: 3,
      is_uncertain: false,
      resolved: false,
    },
  ],
};

it('печатает оглавление подрубрик в печатном порядке и без повторов', () => {
  render(
    <MemoryRouter>
      <ConceptArticleBlock
        article={article}
        rubric=""
        rubricPath={[]}
        volume=""
        onFilterChange={() => {}}
        loadedRefs={new Set()}
      />
    </MemoryRouter>,
  );
  const toc = screen.getByRole('navigation', { name: 'Подрубрики' });
  const items = [...toc.querySelectorAll('li')].map((li) => li.textContent);
  expect(items).toEqual(['определение', 'его мера']);
});

// Работы-указателя в корпусе нет вовсе у целого издания (весь ленинский
// указатель). На внешний сайт, с которого снят указатель, читальня не
// ссылается (решение владельца 24.09.2026) — даже если source_url ещё
// приезжает из строк, ввезённых до этого решения.
const articleWithoutWork: ConceptArticle = {
  ...article,
  work_id: undefined,
  source_url: 'https://example.org/lenin-index.pdf',
};

it('без work_id не рисует ссылку на источник, даже если пришёл source_url', () => {
  render(
    <MemoryRouter>
      <ConceptArticleBlock
        article={articleWithoutWork}
        rubric=""
        rubricPath={[]}
        volume=""
        onFilterChange={() => {}}
        loadedRefs={new Set()}
      />
    </MemoryRouter>,
  );
  expect(screen.queryByText(/Указатель, стр\./)).not.toBeInTheDocument();
  expect(screen.queryByText('Источник указателя')).not.toBeInTheDocument();
  expect(document.querySelector('a[href="https://example.org/lenin-index.pdf"]')).toBeNull();
});

it('под фильтром тома оглавление не держит подрубрик без адресов в этом томе', () => {
  render(
    <MemoryRouter>
      <ConceptArticleBlock
        article={article}
        rubric=""
        rubricPath={[]}
        volume="12"
        onFilterChange={() => {}}
        loadedRefs={new Set()}
      />
    </MemoryRouter>,
  );
  // «его мера» есть только в т. 13: пункт вёл бы в пустую выдачу.
  const toc = screen.getByRole('navigation', { name: 'Подрубрики' });
  expect([...toc.querySelectorAll('li')].map((li) => li.textContent)).toEqual(['определение']);
});

// Фильтр подрубрики оглавление НЕ сужает: иначе после первого клика в нём
// оставался бы один пункт, и перейти к соседней подрубрике можно было бы
// только через «все» в списке.
it('под фильтром подрубрики оглавление держит все пункты и отмечает выбранный', () => {
  render(
    <MemoryRouter>
      <ConceptArticleBlock
        article={article}
        rubric=""
        rubricPath={['его мера']}
        volume=""
        onFilterChange={() => {}}
        loadedRefs={new Set()}
      />
    </MemoryRouter>,
  );
  const toc = screen.getByRole('navigation', { name: 'Подрубрики' });
  const buttons = within(toc).getAllByRole('button');
  expect(buttons.map((b) => [b.textContent, b.getAttribute('aria-pressed')])).toEqual([
    ['определение', 'false'],
    ['его мера', 'true'],
  ]);
});

it('повторный клик по выбранному пункту оглавления снимает фильтр подрубрики', async () => {
  const onFilterChange = vi.fn();
  render(
    <MemoryRouter>
      <ConceptArticleBlock
        article={article}
        rubric=""
        rubricPath={['его мера']}
        volume="13"
        onFilterChange={onFilterChange}
        loadedRefs={new Set()}
      />
    </MemoryRouter>,
  );
  const toc = screen.getByRole('navigation', { name: 'Подрубрики' });
  await userEvent.click(within(toc).getByRole('button', { name: 'его мера' }));
  expect(onFilterChange).toHaveBeenCalledWith({ rubric: '', rubricPath: [], volume: '13' });
});

// Пункт оглавления сужает И панель, И поток справа. Прежде это был якорь
// внутри самой панели: левый список прыгал к подрубрике, а поток оставался
// на месте — нужная подрубрика могла начинаться с 192-й записи из 247, а
// поток грузится порциями по 20, так что прыгать справа было не к чему.
it('пункт оглавления ставит фильтр по пути подрубрики, не трогая том', async () => {
  const onFilterChange = vi.fn();
  render(
    <MemoryRouter>
      <ConceptArticleBlock
        article={article}
        rubric=""
        rubricPath={[]}
        volume="13"
        onFilterChange={onFilterChange}
        loadedRefs={new Set()}
      />
    </MemoryRouter>,
  );
  const toc = screen.getByRole('navigation', { name: 'Подрубрики' });
  await userEvent.click(within(toc).getByRole('button', { name: 'его мера' }));
  expect(onFilterChange).toHaveBeenCalledWith({
    rubric: '',
    rubricPath: ['его мера'],
    volume: '13',
  });
});

// Пришедшая закладка со старым ?rubric= тоже вытесняется путём — тем же
// правилом, что у выбора в списке: два фильтра разом не сужают.
it('пункт оглавления вытесняет плоскую закладку путём', async () => {
  const onFilterChange = vi.fn();
  render(
    <MemoryRouter>
      <ConceptArticleBlock
        article={article}
        rubric="его мера"
        rubricPath={[]}
        volume=""
        onFilterChange={onFilterChange}
        loadedRefs={new Set()}
      />
    </MemoryRouter>,
  );
  const toc = screen.getByRole('navigation', { name: 'Подрубрики' });
  await userEvent.click(within(toc).getByRole('button', { name: 'его мера' }));
  expect(onFilterChange).toHaveBeenCalledWith({
    rubric: '',
    rubricPath: ['его мера'],
    volume: '',
  });
});

// «КПСС — съезды»: один и тот же лист-аспект стоит под разными съездами —
// задача 9/10. Проверяем ровно то, что называет бриф: запись с путём длины 2
// рисуется ВНУТРИ своего подзаголовка (вложенный .concept-subrubric с
// собственным h4), а не наравне с ним (не как ещё один .concept-rubric с h3).
const articleWithNestedRubrics: ConceptArticle = {
  ...article,
  references: [
    {
      id: 10,
      article_id: 1,
      volume_number: 4,
      page_start: 1,
      page_end: 1,
      rubric: 'значение съезда',
      rubric_path: ['I съезд РСДРП', 'значение съезда'],
      order_number: 1,
      is_uncertain: false,
      resolved: false,
    },
    {
      id: 11,
      article_id: 1,
      volume_number: 41,
      page_start: 2,
      page_end: 2,
      rubric: 'значение съезда',
      rubric_path: ['II съезд РСДРП', 'значение съезда'],
      order_number: 2,
      is_uncertain: false,
      resolved: false,
    },
  ],
};

it('запись с путём длины 2 рисуется внутри своего подзаголовка, а не наравне с ним', () => {
  render(
    <MemoryRouter>
      <ConceptArticleBlock
        article={articleWithNestedRubrics}
        rubric=""
        rubricPath={[]}
        volume=""
        onFilterChange={() => {}}
        loadedRefs={new Set()}
      />
    </MemoryRouter>,
  );
  // Верхний уровень — съезды (h3), не слитый по листу «значение съезда».
  const topHeadings = [...document.querySelectorAll('.concept-rubric > h3')].map(
    (h) => h.textContent,
  );
  expect(topHeadings).toEqual(['I съезд РСДРП', 'II съезд РСДРП']);
  // Внутри каждого съезда — свой вложенный подзаголовок с тем же именем листа,
  // не два адреса вперемешку под одним заголовком.
  const subHeadings = [...document.querySelectorAll('.concept-subrubric > h4')].map(
    (h) => h.textContent,
  );
  expect(subHeadings).toEqual(['значение съезда', 'значение съезда']);
  // Оба независимых экземпляра «значение съезда» — вложены, каждый внутри
  // своего съезда, а не два .concept-rubric верхнего уровня.
  expect(document.querySelectorAll('.concept-rubric').length).toBe(2);
  const subrubrics = [...document.querySelectorAll('.concept-subrubric')];
  expect(subrubrics.length).toBe(2);
  // Якорь вложенной подрубрики несёт весь путь, а не только лист: у обоих
  // «значение съезда» разные родители, поэтому id обязаны различаться — иначе
  // ссылка на второй съезд вела бы в первый, а document.getElementById видел
  // бы только первый узел.
  const subrubricIds = subrubrics.map((el) => el.id);
  expect(subrubricIds.every((id) => id !== '')).toBe(true);
  expect(new Set(subrubricIds).size).toBe(subrubricIds.length);
  // У съезда самого по себе нет своих адресов — всё ушло во вложенный
  // «значение съезда»: пустого списка адресов на этом уровне рисоваться не
  // должно.
  const topGroups = [...document.querySelectorAll('.concept-rubric')];
  for (const top of topGroups) {
    expect(top.querySelector(':scope > ul.concept-ref-list')).toBeNull();
  }
  // Оглавление ведёт на съезды, а не на семь одинаковых «значение съезда».
  const toc = screen.getByRole('navigation', { name: 'Подрубрики' });
  expect([...toc.querySelectorAll('li')].map((li) => li.textContent)).toEqual([
    'I съезд РСДРП',
    'II съезд РСДРП',
  ]);
});

/** Адрес с явным путём; плоский rubric — лист пути, как его отдаёт сервер. */
function refAt(id: number, path: string[]): ConceptReference {
  return {
    id,
    article_id: 1,
    volume_number: 4,
    page_start: id,
    page_end: id,
    rubric: path[path.length - 1] ?? '',
    rubric_path: path,
    order_number: id,
    is_uncertain: false,
    resolved: false,
  };
}

/** Блок статьи с подменёнными адресами и фильтрами; остальное — из article. */
function block(over: {
  references: ConceptReference[];
  rubric?: string;
  rubricPath?: string[];
  volume?: string;
}) {
  return (
    <MemoryRouter>
      <ConceptArticleBlock
        article={{ ...article, references: over.references }}
        rubric={over.rubric ?? ''}
        rubricPath={over.rubricPath ?? []}
        volume={over.volume ?? ''}
        onFilterChange={() => {}}
        loadedRefs={new Set()}
      />
    </MemoryRouter>
  );
}

it('съезд с детьми даёт группу, внутри — «весь раздел» и аспекты', () => {
  render(
    block({
      references: [
        refAt(1, ['II съезд РСДРП', 'значение съезда']),
        refAt(2, ['II съезд РСДРП', 'о Бунде']),
      ],
    }),
  );

  const select = screen.getByLabelText('подрубрика');
  const group = select.querySelector('optgroup');
  expect(group?.label).toBe('II съезд РСДРП');
  expect([...group!.querySelectorAll('option')].map((o) => o.textContent)).toEqual([
    'весь раздел',
    'значение съезда',
    'о Бунде',
  ]);
});

it('выбор «весь раздел» показывает и свой адрес съезда, и все его аспекты', () => {
  render(
    block({
      references: [
        refAt(1, ['II съезд РСДРП']),
        refAt(2, ['II съезд РСДРП', 'значение съезда']),
        refAt(3, ['III съезд РСДРП', 'значение съезда']),
      ],
      rubricPath: ['II съезд РСДРП'],
    }),
  );

  expect(screen.getByText(/показано 2 из 3 адресов/)).toBeTruthy();
});

it('выбор аспекта внутри съезда не зачерпывает одноимённый аспект другого съезда', () => {
  // Ровно тот дефект, ради которого работа и делается: «значение съезда»
  // стоит под семью съездами, и плоское сравнение по листу сливало их в
  // одно. Путь длиной ДВА звена здесь обязателен — на пути из одного звена
  // сравнение по префиксу и сравнение «путь содержит лист» дают одно и то
  // же, и подмена одного другим осталась бы незамеченной.
  render(
    block({
      references: [
        refAt(1, ['II съезд РСДРП', 'значение съезда']),
        refAt(2, ['III съезд РСДРП', 'значение съезда']),
      ],
      rubricPath: ['II съезд РСДРП', 'значение съезда'],
    }),
  );

  expect(screen.getByText(/показано 1 из 2 адресов/)).toBeTruthy();
});

// Чужая закладка со старым ?rubric= обязана открываться, как открывалась:
// выбор листа зачерпывает его из ВСЕХ съездов.
it('старый плоский фильтр по листу работает как прежде', () => {
  render(
    block({
      references: [
        refAt(1, ['II съезд РСДРП', 'значение съезда']),
        refAt(2, ['III съезд РСДРП', 'значение съезда']),
        refAt(3, ['III съезд РСДРП', 'о Бунде']),
      ],
      rubric: 'значение съезда',
    }),
  );

  expect(screen.getByText(/показано 2 из 3 адресов/)).toBeTruthy();
});

// Блокер финальной рецензии: при адресе ?rubric=определение путь пуст, и
// список ошибочно показывал выбранной опцию «все», хотя выдача сужена.
// Проверяем ИМЕННО выбранную опцию (value и текст), а не то, что опция
// «определение» просто существует в разметке — существование не ловит эту
// мутацию, значение атрибута — ловит.
it('пришедшая закладка ?rubric= показывает выбранным свой лист, а не «все»', () => {
  render(
    <MemoryRouter>
      <ConceptArticleBlock
        article={article}
        rubric="определение"
        rubricPath={[]}
        volume=""
        onFilterChange={() => {}}
        loadedRefs={new Set()}
      />
    </MemoryRouter>,
  );
  const select = screen.getByLabelText('подрубрика') as HTMLSelectElement;
  expect(select.selectedIndex).not.toBe(-1);
  const selected = select.options[select.selectedIndex];
  expect(selected.textContent).toBe('определение');
});

// Продолжение той же находки: список обязан уметь СНЯТЬ пришедшую закладку
// выбором «все». Старый тест ConceptView («выбор подрубрики пишет путь...»)
// дёргает change безусловно через userEvent и потому проходит независимо от
// того, какая опция была выбрана до этого — эта мутация ему не видна.
it('выбор «все» при пришедшей закладке ?rubric= сбрасывает и rubric, и rubricPath', async () => {
  const onFilterChange = vi.fn();
  render(
    <MemoryRouter>
      <ConceptArticleBlock
        article={article}
        rubric="определение"
        rubricPath={[]}
        volume=""
        onFilterChange={onFilterChange}
        loadedRefs={new Set()}
      />
    </MemoryRouter>,
  );
  const select = screen.getByLabelText('подрубрика');
  await userEvent.selectOptions(select, 'все');
  expect(onFilterChange).toHaveBeenCalledWith({ rubric: '', rubricPath: [], volume: '' });
});

// Третий тест сверх обязательных двух: тест (б) выше не различает старый и
// починенный обработчик — при value="" (опция «все») оба дают один и тот же
// вызов, decodeRubricPath("") === []. Разница обработчика видна только на
// самом сентинеле: decodeRubricPath('\u0000flat') отдаёт ['\u0000flat'], а
// не [] (проверено отдельно) — без ветки `e.target.value === FLAT_BOOKMARK`
// повторный выбор уже стоящей закладки протащил бы сентинел в rubricPath как
// обычный путь.
it('повторный выбор уже показанной плоской закладки не декодирует сентинел как путь', async () => {
  const onFilterChange = vi.fn();
  render(
    <MemoryRouter>
      <ConceptArticleBlock
        article={article}
        rubric="определение"
        rubricPath={[]}
        volume=""
        onFilterChange={onFilterChange}
        loadedRefs={new Set()}
      />
    </MemoryRouter>,
  );
  const select = screen.getByLabelText('подрубрика');
  await userEvent.selectOptions(select, '\u0000flat');
  expect(onFilterChange).toHaveBeenCalledWith({ rubric: '', rubricPath: [], volume: '' });
});

it('при обоих фильтрах побеждает путь, а не плоское имя', () => {
  // Лист второго адреса намеренно НЕ совпадает с плоским фильтром: иначе
  // тест не отличит победу пути от простого совпадения по листу, и подмена
  // `else if` на `if` прошла бы незамеченной (та же дыра, что чинилась на
  // сервере в TestCollectEntriesRubricPathBeatsFlatRubric).
  render(
    block({
      references: [
        refAt(1, ['II съезд РСДРП', 'значение съезда']),
        refAt(2, ['III съезд РСДРП', 'организационные вопросы']),
      ],
      rubric: 'значение съезда',
      rubricPath: ['III съезд РСДРП'],
    }),
  );

  expect(screen.getByText(/показано 1 из 2 адресов/)).toBeTruthy();
});

it('статья без вложенности не получает ни одной группы', () => {
  const { container } = render(
    block({
      references: [refAt(1, ['определение']), refAt(2, ['его мера'])],
    }),
  );

  expect(container.querySelectorAll('optgroup')).toHaveLength(0);
  expect([...container.querySelectorAll('select')][0].querySelectorAll('option')).toHaveLength(3);
});
