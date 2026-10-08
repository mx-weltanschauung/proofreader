import { describe, expect, it, vi } from 'vitest';
import { render } from '@testing-library/react';
import { useRef } from 'react';
import { useQuoteHighlight } from './useQuoteHighlight';
import { markQuote, type QuoteMatch } from '../utils/quoteMatch';

// Настоящая реализация, обёрнутая в счётчик: поведение то же, но видно,
// СКОЛЬКО раз хук пошёл в DOM. Без этого «зависимость по полям пары, а не по
// объекту» — непроверяемое утверждение в комментарии, а цена ошибки —
// flattenText по всей главе на каждый рендер читалки.
vi.mock('../utils/quoteMatch', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../utils/quoteMatch')>();
  return { ...actual, markQuote: vi.fn(actual.markQuote) };
});

/** Проба: реф на контейнер, текст — в дочернем .page-html-content (как у PageView). */
const Probe: React.FC<{
  quote: string;
  end?: string;
  pageNumber?: number;
  html: string;
  depKey: string;
  onMatch: (m: QuoteMatch | null) => void;
}> = ({ quote, end, pageNumber, html, depKey, onMatch }) => {
  const ref = useRef<HTMLDivElement>(null);
  const match = useQuoteHighlight(ref, { start: quote, end }, pageNumber, [depKey]);
  onMatch(match);
  return (
    <div ref={ref} className="page-html-content" data-page={pageNumber}>
      <div dangerouslySetInnerHTML={{ __html: html }} />
    </div>
  );
};

/** Проба читалки: две полосы главы, каждая — своя секция с data-page. */
const ChapterProbe: React.FC<{
  quote: string;
  pageNumber?: number;
  onMatch: (m: QuoteMatch | null) => void;
}> = ({ quote, pageNumber, onMatch }) => {
  const ref = useRef<HTMLDivElement>(null);
  const match = useQuoteHighlight(ref, { start: quote }, pageNumber, ['x']);
  onMatch(match);
  return (
    <div ref={ref}>
      <div data-page={1} className="page-html-content">
        <p>Так писал он в тысяча восемьсот сорок восьмом году.</p>
      </div>
      <div data-page={2} className="page-html-content">
        <p>Так писал он в тысяча восемьсот сорок восьмом году.</p>
      </div>
    </div>
  );
};

describe('useQuoteHighlight', () => {
  it('без ?quote= не ходит в DOM вовсе', () => {
    const onMatch = vi.fn();
    const { container } = render(
      <Probe quote="" html="<p>Гегеля читали</p>" depKey="a" onMatch={onMatch} />,
    );
    expect(container.querySelector('mark.quote-hit')).toBeNull();
    expect(onMatch).toHaveBeenLastCalledWith(null);
  });

  it('подсвечивает найденное', () => {
    const onMatch = vi.fn();
    const { container } = render(
      <Probe
        quote="Гегеля читали"
        html="<p>Гегеля читали, а ёлка стояла.</p>"
        depKey="a"
        pageNumber={1}
        onMatch={onMatch}
      />,
    );
    expect(container.querySelector('mark.quote-hit')?.textContent).toBe('Гегеля читали');
    expect(onMatch).toHaveBeenLastCalledWith('hit');
  });

  it('несколько вхождений на всём тексте — multiple, подсвечено первое', () => {
    const onMatch = vi.fn();
    render(
      <Probe
        quote="Гегеля"
        html="<p>Гегеля и снова Гегеля.</p>"
        depKey="a"
        pageNumber={1}
        onMatch={onMatch}
      />,
    );
    expect(onMatch).toHaveBeenLastCalledWith('multiple');
  });

  it('не найденное — miss', () => {
    const onMatch = vi.fn();
    render(
      <Probe
        quote="Отсутствующий текст"
        html="<p>Гегеля читали</p>"
        depKey="a"
        pageNumber={1}
        onMatch={onMatch}
      />,
    );
    expect(onMatch).toHaveBeenLastCalledWith('miss');
  });

  // Решение рецензента: адрес без места ничего не подтверждает — подсветка
  // первого совпадения в главе (без знания, какая именно полоса имелась в
  // виду) хуже честного бездействия. Мутация «искать и без pageNumber»
  // красит этот тест: quote совпадает с html дословно, и без исправления
  // здесь оказался бы 'hit', а не null.
  it('без номера полосы поиск не идёт вовсе — не гадает место', () => {
    const onMatch = vi.fn();
    const { container } = render(
      <Probe
        quote="Гегеля читали"
        html="<p>Гегеля читали, а ёлка стояла.</p>"
        depKey="a"
        onMatch={onMatch}
      />,
    );
    expect(container.querySelector('mark.quote-hit')).toBeNull();
    expect(onMatch).toHaveBeenLastCalledWith(null);
  });

  // Не декорация, а условие правильности: та же фраза стоит на ДВУХ полосах
  // главы, и без сужения по номеру полосы поиск по всему тексту законно
  // вернул бы 'multiple' — тот же дефектный класс, что дал бы ссылке на
  // конкретное место указателя (задача 14) неоднозначный результат вместо
  // точного попадания. Если pageNumber не дойдёт до markQuote (мутация),
  // результат здесь станет 'multiple', и тест покраснеет.
  it('номер полосы сужает поиск — та же фраза на другой полосе не мешает', () => {
    const onMatch = vi.fn();
    const { container } = render(
      <ChapterProbe
        quote="Так писал он в тысяча восемьсот сорок восьмом году."
        pageNumber={2}
        onMatch={onMatch}
      />,
    );
    expect(onMatch).toHaveBeenLastCalledWith('hit');
    const mark = container.querySelector('mark.quote-hit');
    expect(mark?.closest('[data-page]')?.getAttribute('data-page')).toBe('2');
  });

  // Смена pageNumber БЕЗ замены innerHTML (тот же DOM ChapterProbe, deps не
  // меняются) — единственный живой путь к повторному markQuote на уже
  // размеченном дереве. Без unmarkQuoteHits прежний <mark> с полосы 1
  // остаётся в дереве нетронутым, и после переключения на полосу 2 в главе
  // повисает два <mark class="quote-hit"> вместо одного актуального.
  it('смена номера полосы без замены содержимого не оставляет старую подсветку', () => {
    const onMatch = vi.fn();
    const { container, rerender } = render(
      <ChapterProbe
        quote="Так писал он в тысяча восемьсот сорок восьмом году."
        pageNumber={1}
        onMatch={onMatch}
      />,
    );
    expect(onMatch).toHaveBeenLastCalledWith('hit');

    rerender(
      <ChapterProbe
        quote="Так писал он в тысяча восемьсот сорок восьмом году."
        pageNumber={2}
        onMatch={onMatch}
      />,
    );
    expect(onMatch).toHaveBeenLastCalledWith('hit');
    expect(container.querySelectorAll('mark.quote-hit')).toHaveLength(1);
    expect(
      container.querySelector('mark.quote-hit')?.closest('[data-page]')?.getAttribute('data-page'),
    ).toBe('2');
  });
});

describe('useQuoteHighlight: пара якорей', () => {
  it('подсвечивает пролёт от начала до конца, а не один ключ', () => {
    const onMatch = vi.fn();
    const { container } = render(
      <Probe
        quote="Если до"
        end="воспитан правильно."
        html="<p>Если до 6 лет ребенок воспитан правильно. Дальше не важно.</p>"
        depKey="a"
        pageNumber={1}
        onMatch={onMatch}
      />,
    );
    const marks = [...container.querySelectorAll('mark.quote-hit')];
    expect(marks.map((m) => m.textContent).join('')).toBe(
      'Если до 6 лет ребенок воспитан правильно.',
    );
    expect(onMatch).toHaveBeenLastCalledWith('hit');
  });

  it('ненайденный конец даёт partial — читателю скажут, что текст правили', () => {
    const onMatch = vi.fn();
    const { container } = render(
      <Probe
        quote="Если до"
        end="этого тут больше нет"
        html="<p>Если до 6 лет ребенок воспитан иначе.</p>"
        depKey="a"
        pageNumber={1}
        onMatch={onMatch}
      />,
    );
    expect(container.querySelector('mark.quote-hit')?.textContent).toBe('Если до');
    expect(onMatch).toHaveBeenLastCalledWith('partial');
  });

  // Смена одного якоря конца — тоже новый адрес: эффект обязан перезапуститься,
  // иначе подсветка останется от прежней ссылки. Зависимость идёт по ПОЛЯМ
  // пары, а не по объекту: объект собирается на каждый рендер заново, и
  // зависимость по нему перезапускала бы эффект всегда.
  it('смена только якоря конца пересобирает подсветку', () => {
    const onMatch = vi.fn();
    const html = '<p>Если до 6 лет ребенок воспитан правильно. Дальше не важно.</p>';
    const { container, rerender } = render(
      <Probe quote="Если до" html={html} depKey="a" pageNumber={1} onMatch={onMatch} />,
    );
    expect(container.querySelector('mark.quote-hit')?.textContent).toBe('Если до');

    rerender(
      <Probe quote="Если до" end="6 лет" html={html} depKey="a" pageNumber={1} onMatch={onMatch} />,
    );
    const marks = [...container.querySelectorAll('mark.quote-hit')];
    expect(marks.map((m) => m.textContent).join('')).toBe('Если до 6 лет');
  });

  it('рендер с теми же якорями не ходит в DOM второй раз', () => {
    vi.mocked(markQuote).mockClear();
    const html = '<p>Если до 6 лет ребенок воспитан правильно.</p>';
    const props = {
      quote: 'Если до',
      end: '6 лет',
      html,
      depKey: 'a',
      pageNumber: 1,
      onMatch: vi.fn(),
    };
    const { rerender } = render(<Probe {...props} />);
    expect(markQuote).toHaveBeenCalledTimes(1);

    rerender(<Probe {...props} />);
    expect(markQuote).toHaveBeenCalledTimes(1);
  });
});

// Прокрутки мало: на месте цитаты должен оказаться и фокус. Иначе он
// остаётся там, куда его поставил якорь полосы (useHashAnchor -> jumpToAnchor),
// то есть на секции или шве ВЫШЕ цитаты, — и читатель видит кольцо
// `:focus-visible` вокруг соседнего абзаца, а скринридер встаёт не на том
// месте, на которое сослались.
describe('useQuoteHighlight: фокус на найденном месте', () => {
  it('переносит фокус на первую подсветку', () => {
    const { container } = render(
      <Probe
        quote="Гегеля читали"
        html="<p>Гегеля читали, а ёлка стояла.</p>"
        depKey="a"
        pageNumber={5}
        onMatch={vi.fn()}
      />,
    );

    expect(document.activeElement).toBe(container.querySelector('mark.quote-hit'));
  });

  it('промах фокус не трогает', () => {
    const outside = document.createElement('button');
    document.body.append(outside);
    outside.focus();

    render(
      <Probe
        quote="Отсутствующий текст"
        html="<p>Гегеля читали</p>"
        depKey="a"
        pageNumber={5}
        onMatch={vi.fn()}
      />,
    );

    expect(document.activeElement).toBe(outside);
    outside.remove();
  });

  // Тот же счёт «один раз за жизнь компонента», что и у прокрутки: читатель,
  // ушедший фокусом на кнопку панели, не должен получать его обратно на
  // каждую перерисовку читалки.
  it('не отбирает фокус обратно при смене цитаты и содержимого', () => {
    const props = {
      quote: 'Гегеля читали',
      html: '<p>Гегеля читали, а ёлка стояла.</p>',
      depKey: 'a',
      pageNumber: 5,
      onMatch: vi.fn(),
    };
    const { rerender } = render(<Probe {...props} />);

    const outside = document.createElement('button');
    document.body.append(outside);
    outside.focus();

    rerender(<Probe {...props} quote="ёлка" />);
    expect(document.activeElement).toBe(outside);

    rerender(
      <Probe {...props} quote="ёлка" html="<p>Другой текст с ёлкой опять.</p>" depKey="b" />,
    );
    expect(document.activeElement).toBe(outside);

    outside.remove();
  });
});

describe('useQuoteHighlight: прокрутка к первому совпадению', () => {
  // jsdom не реализует scrollIntoView — src/test/setup.ts подменяет его на
  // no-op глобально, так что счётчик вызовов нужен свой, тот же приём, что и
  // в useSearchHighlight.test.tsx.
  it('прокручивает один раз и не повторяет при смене цитаты и содержимого', () => {
    const spy = vi.spyOn(Element.prototype, 'scrollIntoView').mockImplementation(function (
      this: Element,
    ) {
      void this;
    });

    const { rerender } = render(
      <Probe
        quote="Гегеля читали"
        html="<p>Гегеля читали, а ёлка стояла.</p>"
        depKey="a"
        pageNumber={5}
        onMatch={vi.fn()}
      />,
    );
    expect(spy).toHaveBeenCalledTimes(1);

    // Смена цитаты: находится новое совпадение, но прокрутка не должна
    // повториться — «один раз за жизнь компонента», а не «один раз на цитату».
    rerender(
      <Probe
        quote="ёлка"
        html="<p>Гегеля читали, а ёлка стояла.</p>"
        depKey="a"
        pageNumber={5}
        onMatch={vi.fn()}
      />,
    );
    expect(spy).toHaveBeenCalledTimes(1);

    // Смена содержимого (аналог новой полосы) с новой цитатой — тоже не
    // должна повторить прокрутку.
    rerender(
      <Probe
        quote="ёлка"
        html="<p>Другой текст с ёлкой опять.</p>"
        depKey="b"
        pageNumber={5}
        onMatch={vi.fn()}
      />,
    );
    expect(spy).toHaveBeenCalledTimes(1);

    spy.mockRestore();
  });

  it('промах не прокручивает вовсе', () => {
    const spy = vi.spyOn(Element.prototype, 'scrollIntoView').mockImplementation(function (
      this: Element,
    ) {
      void this;
    });

    render(
      <Probe
        quote="Отсутствующий текст"
        html="<p>Гегеля читали</p>"
        depKey="a"
        pageNumber={5}
        onMatch={vi.fn()}
      />,
    );
    expect(spy).not.toHaveBeenCalled();

    spy.mockRestore();
  });
});
