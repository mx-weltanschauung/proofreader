import { describe, expect, it } from 'vitest';
import type { ConceptReference } from '../types';
import { groupByRubric, rubricOptions } from './conceptReferences';

function ref(overrides: Partial<ConceptReference> & { id: number }): ConceptReference {
  return {
    article_id: 1,
    volume_number: 12,
    page_start: 1,
    page_end: 1,
    rubric: '',
    order_number: overrides.id,
    is_uncertain: false,
    resolved: false,
    ...overrides,
  };
}

describe('groupByRubric', () => {
  it('путь длины 1 группирует как раньше — по листу, плоско, без детей', () => {
    const refs = [
      ref({ id: 1, rubric: 'определение', order_number: 1 }),
      ref({ id: 2, rubric: 'его мера', order_number: 2 }),
      ref({ id: 3, rubric: 'определение', order_number: 3 }),
    ];
    const groups = groupByRubric(refs);
    expect(groups.map((g) => g.rubric)).toEqual(['определение', 'его мера']);
    expect(groups[0].children).toEqual([]);
    expect(groups[0].refs.map((r) => r.id)).toEqual([1, 3]);
    expect(groups[1].children).toEqual([]);
    expect(groups[1].refs.map((r) => r.id)).toEqual([2]);
  });

  it('путь длины 2 рисуется внутри своего подзаголовка, а не наравне с ним', () => {
    // «КПСС — съезды»: один и тот же лист («значение съезда») стоит под
    // разными съездами — слить их по листу значило бы перепутать адреса
    // разных съездов в одну кучу.
    const refs = [
      ref({
        id: 1,
        rubric: 'значение съезда',
        rubric_path: ['I съезд РСДРП', 'значение съезда'],
        order_number: 1,
      }),
      ref({
        id: 2,
        rubric: 'значение съезда',
        rubric_path: ['II съезд РСДРП', 'значение съезда'],
        order_number: 2,
      }),
      ref({
        id: 3,
        rubric: 'организационные вопросы',
        rubric_path: ['I съезд РСДРП', 'организационные вопросы'],
        order_number: 3,
      }),
    ];
    const groups = groupByRubric(refs);
    // Верхний уровень — съезды, а не слитые в одну группу листья.
    expect(groups.map((g) => g.rubric)).toEqual(['I съезд РСДРП', 'II съезд РСДРП']);
    // У верхней группы нет своих адресов — все ушли во вложенные подрубрики.
    expect(groups[0].refs).toEqual([]);
    expect(groups[0].children.map((c) => c.rubric)).toEqual([
      'значение съезда',
      'организационные вопросы',
    ]);
    expect(groups[0].children[0].refs.map((r) => r.id)).toEqual([1]);
    expect(groups[0].children[1].refs.map((r) => r.id)).toEqual([3]);
    // Второй съезд несёт СВОЙ «значение съезда» — не тот же адрес, что у первого.
    expect(groups[1].children.map((c) => c.rubric)).toEqual(['значение съезда']);
    expect(groups[1].children[0].refs.map((r) => r.id)).toEqual([2]);
    // Путь до вложенной группы несёт обоих родителей — нужен для якоря,
    // иначе «значение съезда» двух съездов получили бы один и тот же id.
    expect(groups[0].children[0].path).toEqual(['I съезд РСДРП', 'значение съезда']);
    expect(groups[1].children[0].path).toEqual(['II съезд РСДРП', 'значение съезда']);
  });

  it('рубрика несёт и свои адреса, и вложенные подрубрики одновременно', () => {
    // Съезд может быть упомянут и сам по себе (путь длины 1), и через свой
    // аспект (путь длины 2) — оба существующих теста строят сценарий, где у
    // группы верхнего уровня refs пуст; здесь он непуст одновременно с
    // непустыми children.
    const refs = [
      ref({
        id: 1,
        rubric: 'I съезд РСДРП',
        rubric_path: ['I съезд РСДРП'],
        order_number: 1,
      }),
      ref({
        id: 2,
        rubric: 'значение съезда',
        rubric_path: ['I съезд РСДРП', 'значение съезда'],
        order_number: 2,
      }),
    ];
    const groups = groupByRubric(refs);
    expect(groups.map((g) => g.rubric)).toEqual(['I съезд РСДРП']);
    expect(groups[0].refs.map((r) => r.id)).toEqual([1]);
    expect(groups[0].children.map((c) => c.rubric)).toEqual(['значение съезда']);
    expect(groups[0].children[0].refs.map((r) => r.id)).toEqual([2]);
  });

  it('без rubric_path, но с пустым rubric — безрубричная группа, как раньше', () => {
    const refs = [ref({ id: 1, rubric: '' })];
    const groups = groupByRubric(refs);
    expect(groups).toEqual([{ rubric: '', path: [], refs, children: [] }]);
  });

  it('глубина 3 не ломает группировку (запрос рекурсивный, потолка в схеме нет)', () => {
    const refs = [
      ref({
        id: 1,
        rubric: 'в',
        rubric_path: ['а', 'б', 'в'],
      }),
    ];
    const groups = groupByRubric(refs);
    expect(groups.map((g) => g.rubric)).toEqual(['а']);
    expect(groups[0].children.map((g) => g.rubric)).toEqual(['б']);
    expect(groups[0].children[0].children.map((g) => g.rubric)).toEqual(['в']);
    expect(groups[0].children[0].children[0].refs.map((r) => r.id)).toEqual([1]);
    expect(groups[0].children[0].children[0].path).toEqual(['а', 'б', 'в']);
  });
});

describe('rubricOptions', () => {
  it('корень с детьми даёт группу с «весь раздел» впереди, корень без детей — обычную опцию', () => {
    const refs = [
      ref({ id: 1, rubric: 'съезд партии как ее верховный орган', order_number: 1 }),
      ref({
        id: 2,
        rubric: 'значение съезда',
        rubric_path: ['II съезд РСДРП', 'значение съезда'],
        order_number: 2,
      }),
      ref({
        id: 3,
        rubric: 'о Бунде',
        rubric_path: ['II съезд РСДРП', 'о Бунде'],
        order_number: 3,
      }),
    ];

    expect(rubricOptions(refs)).toEqual([
      {
        label: '',
        options: [
          {
            label: 'съезд партии как ее верховный орган',
            path: ['съезд партии как ее верховный орган'],
          },
        ],
      },
      {
        label: 'II съезд РСДРП',
        options: [
          { label: 'весь раздел', path: ['II съезд РСДРП'] },
          { label: 'значение съезда', path: ['II съезд РСДРП', 'значение съезда'] },
          { label: 'о Бунде', path: ['II съезд РСДРП', 'о Бунде'] },
        ],
      },
    ]);
  });

  it('съезд без единого своего адреса всё равно выбирается — через «весь раздел»', () => {
    // Живой случай: у I, VII и VIII съездов своих адресов ноль, весь материал
    // в аспектах. В плоском списке они не появлялись вовсе.
    const refs = [
      ref({
        id: 1,
        rubric: 'значение съезда',
        rubric_path: ['I съезд РСДРП', 'значение съезда'],
        order_number: 1,
      }),
    ];

    expect(rubricOptions(refs)).toEqual([
      {
        label: 'I съезд РСДРП',
        options: [
          { label: 'весь раздел', path: ['I съезд РСДРП'] },
          { label: 'значение съезда', path: ['I съезд РСДРП', 'значение съезда'] },
        ],
      },
    ]);
  });

  it('адреса без подрубрики опции не получают — их показывает «все»', () => {
    expect(rubricOptions([ref({ id: 1, rubric: '', order_number: 1 })])).toEqual([]);
  });

  it('третий уровень не теряется: optgroup не вкладываются, подпись несёт хвост пути', () => {
    const refs = [ref({ id: 1, rubric: 'в', rubric_path: ['а', 'б', 'в'], order_number: 1 })];

    expect(rubricOptions(refs)).toEqual([
      {
        label: 'а',
        options: [
          { label: 'весь раздел', path: ['а'] },
          { label: 'б', path: ['а', 'б'] },
          { label: 'б — в', path: ['а', 'б', 'в'] },
        ],
      },
    ]);
  });
});
