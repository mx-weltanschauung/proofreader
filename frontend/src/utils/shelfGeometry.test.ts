import { describe, it, expect } from 'vitest';
import {
  type ShelfSlot,
  COLLAPSED_VOLUMES,
  collapseAt,
  mergeGapRuns,
  shelfSlots,
} from './shelfGeometry';

interface Vol {
  volume_number?: number;
  volume_part?: string;
}

const vol = (volume_number: number | undefined, volume_part?: string): Vol => ({
  volume_number,
  volume_part,
});

/** Полка строкой: том — своим номером, пустое место — «·N». */
function layout(slots: ShelfSlot<Vol>[]): string[] {
  return slots.map((s) =>
    s.kind === 'volume' ? String(s.volume.volume_number ?? 'ук') : `·${s.number}`,
  );
}

describe('shelfSlots', () => {
  it('оставляет пустое место там, где тома нет', () => {
    // Ленинское собрание: тома 1, 2, 4 — третьего в читальне нет.
    expect(layout(shelfSlots([vol(1), vol(2), vol(4)]))).toEqual(['1', '2', '·3', '4']);
  });

  it('даёт по месту на каждый отсутствующий том подряд', () => {
    // Два пропуска подряд — два места, а не одно на весь разрыв.
    expect(layout(shelfSlots([vol(1), vol(4)]))).toEqual(['1', '·2', '·3', '4']);
  });

  it('открывает полку пустыми местами, если собрание начинается не с первого тома', () => {
    // Плехановское собрание начинается с пятого тома: четыре места пустуют.
    expect(layout(shelfSlots([vol(5), vol(6)]))).toEqual(['·1', '·2', '·3', '·4', '5', '6']);
  });

  it('ставит книги одного номера рядом, не разделяя их пустым местом', () => {
    // Том 25 собрания Маркса и Энгельса вышел в двух книгах. Вторая книга не
    // должна открыть пустое место на месте собственного тома.
    expect(layout(shelfSlots([vol(1), vol(2, 'II'), vol(2, 'I')]))).toEqual(['1', '2', '2']);
  });

  it('ставит том без номера за последней книгой, не считая его пропуском', () => {
    expect(layout(shelfSlots([vol(1), vol(undefined), vol(3)]))).toEqual(['1', '·2', '3', 'ук']);
  });

  it('не отправляет справочный том за пустые места будущих томов', () => {
    // Справочный том собрания Маркса и Энгельса (указатель) стоит на полке
    // рядом с книгами, а не за двадцатью восемью ненаписанными: у него нет
    // номера, но он есть в читальне, а их нет.
    expect(layout(shelfSlots([vol(1), vol(undefined)], 4))).toEqual(['1', 'ук', '·2', '·3', '·4']);
  });

  it('пустое собрание даёт пустую полку без единого места', () => {
    expect(shelfSlots([])).toEqual([]);
  });

  it('доводит полку до планового объёма собрания', () => {
    // Плехановских томов четыре из двадцати четырёх. Том 15 отсутствует ровно
    // так же, как том 8, и полка обязана говорить о них одно и то же —
    // иначе хвост читается «здесь собрание кончилось», что неправда.
    expect(layout(shelfSlots([vol(1), vol(2)], 4))).toEqual(['1', '2', '·3', '·4']);
  });

  it('без известного плана обрывает полку на последней книге', () => {
    // Знаменателя нет — придумывать длину полки не из чего.
    expect(layout(shelfSlots([vol(1), vol(2)]))).toEqual(['1', '2']);
  });

  it('не обрезает полку планом, который меньше числа залитых томов', () => {
    // План устарел или вписан неверно; выкинуть настоящий том с полки
    // страшнее, чем показать полку длиннее объявленной.
    expect(layout(shelfSlots([vol(1), vol(2), vol(3)], 2))).toEqual(['1', '2', '3']);
  });

  it('кладёт в место саму книгу, а не её копию', () => {
    // Полка отдаёт корешку тот же объект, что пришёл с сервера: карточка по
    // наведению читает из него поля, которых нет в `Positioned`.
    const one = vol(1);
    expect(shelfSlots([one])).toEqual([{ kind: 'volume', volume: one }]);
    expect((shelfSlots([one])[0] as { volume: Vol }).volume).toBe(one);
  });
});

describe('collapseAt', () => {
  /** Собрание из `n` томов подряд. */
  const run = (n: number) => Array.from({ length: n }, (_, i) => vol(i + 1));

  it('режет штабель после десятого тома', () => {
    const slots = shelfSlots(run(45));
    expect(collapseAt(slots)).toBe(COLLAPSED_VOLUMES);
    const shown = layout(slots.slice(0, collapseAt(slots)));
    expect(shown[shown.length - 1]).toBe('10');
  });

  // Ради этого счёт и ведётся по томам: у Плеханова между пятым и
  // четырнадцатым томом девять пустых мест, и свёрнутый по местам штабель
  // показал бы одну книгу вместо десяти.
  it('считает тома, а не места', () => {
    const sparse = shelfSlots([vol(1), vol(12), vol(13), vol(14), vol(15), vol(16)], 30);
    const shown = layout(sparse.slice(0, collapseAt(sparse, 2)));
    expect(shown.filter((s) => !s.startsWith('·'))).toEqual(['1', '12']);
  });

  // Пустые места за последней показанной книгой уходят под кнопку вместе с
  // ней: иначе свёрнутый штабель Плеханова кончался бы десятком пустых полос
  // ни к чему.
  it('срезает пустой хвост вместе со скрытыми томами', () => {
    const slots = shelfSlots(run(6), 40);
    const shown = layout(slots.slice(0, collapseAt(slots, 3)));
    expect(shown[shown.length - 1]).toBe('3');
  });

  it('короткое собрание не сворачивает', () => {
    const slots = shelfSlots(run(12));
    expect(collapseAt(slots)).toBe(slots.length);
  });

  // Кнопка ради одного-двух спрятанных томов — работа читателю без выгоды.
  it('не сворачивает, когда прятать нечего', () => {
    const slots = shelfSlots(run(5));
    expect(collapseAt(slots, 3)).toBe(slots.length);
  });

  it('пустое собрание не сворачивает', () => {
    expect(collapseAt(shelfSlots([]))).toBe(0);
  });
});

describe('mergeGapRuns', () => {
  it('свёртывает прогон пустых мест в одну полосу', () => {
    // У Чернышевского между вторым и седьмым томом четыре пустых места
    // подряд. Четырьмя отдельными полосами они читаются дырой в вёрстке, а
    // одной подписанной — тем, что и есть: четыре тома ещё не сняты.
    const merged = mergeGapRuns(shelfSlots([vol(2), vol(7)]));
    expect(merged.map((s) => s.kind)).toEqual(['run', 'volume', 'run', 'volume']);
    expect(merged[2]).toMatchObject({ kind: 'run', from: 3, to: 6, count: 4 });
  });

  // Порога длины нет: на живом корпусе засечка в одно место оказалась культёй
  // в три пикселя между двумя книгами и читалась дефектом вёрстки.
  it('одиночный пропуск тоже называет словами', () => {
    const merged = mergeGapRuns(shelfSlots([vol(1), vol(3)]));
    expect(merged.map((s) => s.kind)).toEqual(['volume', 'run', 'volume']);
    expect(merged[1]).toMatchObject({ from: 2, to: 2, count: 1 });
  });

  it('соседние пропуски не дробит на отдельные полосы', () => {
    const merged = mergeGapRuns(shelfSlots([vol(1), vol(5)]));
    expect(merged.map((s) => s.kind)).toEqual(['volume', 'run', 'volume']);
    expect(merged[1]).toMatchObject({ from: 2, to: 4, count: 3 });
  });

  it('свёртывает пустой хвост собрания', () => {
    // Плеханов: четыре тома из двадцати четырёх, хвост в десяток мест.
    const merged = mergeGapRuns(shelfSlots([vol(1)], 11));
    expect(merged.map((s) => s.kind)).toEqual(['volume', 'run']);
    expect(merged[1]).toMatchObject({ from: 2, to: 11, count: 10 });
  });

  it('свёртывает пустое начало собрания', () => {
    // Плеханов начинается с пятого тома: первые четыре места — такой же
    // прогон, как дыра в середине, и подпись ему нужна та же.
    const merged = mergeGapRuns(shelfSlots([vol(5), vol(6)]));
    expect(merged.map((s) => s.kind)).toEqual(['run', 'volume', 'volume']);
    expect(merged[0]).toMatchObject({ from: 1, to: 4, count: 4 });
  });

  it('полное собрание не трогает', () => {
    const full = Array.from({ length: 6 }, (_, i) => vol(i + 1));
    const merged = mergeGapRuns(shelfSlots(full, 6));
    expect(merged.map((s) => s.kind)).toEqual(Array(6).fill('volume'));
  });
});
