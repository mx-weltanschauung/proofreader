import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, screen, act, fireEvent } from '@testing-library/react';
import { useRef } from 'react';
import { Link, MemoryRouter } from 'react-router-dom';
import { FeatureHint } from './FeatureHint';
import { HintsProvider } from '../contexts/HintsProvider';
import { HINTS, HINT_APPEAR_MS, HINT_COUNT_MS, HINT_HIDE_MS } from '../hints/registry';
import { readHints } from '../hints/hintsStorage';
import {
  installHintHarness,
  removeHintHarness,
  sayVisible as say,
  advance,
} from '../test/hintHarness';

function Harness() {
  const ref = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button ref={ref} type="button">
        №
      </button>
      <FeatureHint id="page-numbers" anchorRef={ref} />
    </>
  );
}

function setup() {
  return render(
    <MemoryRouter>
      <HintsProvider>
        <Harness />
      </HintsProvider>
    </MemoryRouter>,
  );
}

// Модель WorkDetail.tsx/ChapterView.tsx: орган и его FeatureHint стоят ВНЕ
// <Routes>, поэтому переход по <Link> внутри одного и того же смонтированного
// поддерева не размонтирует их — ровно то, что происходит с карточкой тома
// при переходе между /works/46 и /works/47 (react-router не пересоздаёт
// компонент, когда меняется только параметр совпавшего маршрута), и с
// DownloadMenu/ChapterTocDrawer при переходе между соседними главами.
function PersistentAnchorHarness() {
  const ref = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button ref={ref} type="button">
        глубина
      </button>
      <FeatureHint id="outline-depth" anchorRef={ref} />
      <Link to="/b">дальше</Link>
    </>
  );
}

function setupPersistentAnchor() {
  return render(
    <MemoryRouter initialEntries={['/a']}>
      <HintsProvider>
        <PersistentAnchorHarness />
      </HintsProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  localStorage.clear();
  installHintHarness();
});

afterEach(removeHintHarness);

describe('выноска', () => {
  it('не появляется сразу: экран должен устояться', () => {
    setup();
    say(true);
    advance(HINT_APPEAR_MS - 100);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    advance(200);
    expect(screen.getByRole('status')).toHaveTextContent(HINTS['page-numbers'].text);
  });

  it('не появляется, пока якорь не виден', () => {
    setup();
    advance(HINT_APPEAR_MS + 100);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('гаснет сама и засчитывает показ', () => {
    setup();
    say(true);
    advance(HINT_APPEAR_MS + HINT_HIDE_MS + 100);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    expect(readHints()['page-numbers']).toEqual({ seen: 1, done: false });
  });

  // Панель чтения прячется при прокрутке вниз вместе с кнопкой «№».
  it('уходит вместе с якорем, и слишком короткий показ не засчитывается', () => {
    setup();
    say(true);
    advance(HINT_APPEAR_MS + 100);
    expect(screen.getByRole('status')).toBeInTheDocument();
    advance(HINT_COUNT_MS - 500);
    say(false);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    expect(readHints()['page-numbers']).toBeUndefined();
  });

  it('показ засчитывается, если выноска провисела достаточно', () => {
    setup();
    say(true);
    advance(HINT_APPEAR_MS + HINT_COUNT_MS + 100);
    say(false);
    expect(readHints()['page-numbers']).toEqual({ seen: 1, done: false });
  });

  // ScrollDock прячет свою кнопку возвратом null, а не CSS — орган и его
  // FeatureHint размонтируются целиком, close() никто явно не зовёт. Без
  // крюка в cleanup-е показ, провисевший своё, остался бы незасчитанным, и
  // при следующем монтаже того же органа (прокрутка обратно) координатор
  // выдал бы право экрана заново.
  it('размонтирование после достаточно долгого показа засчитывает его как обычное закрытие', () => {
    const { unmount } = setup();
    say(true);
    advance(HINT_APPEAR_MS + HINT_COUNT_MS + 100);
    expect(screen.getByRole('status')).toBeInTheDocument();
    unmount();
    expect(readHints()['page-numbers']).toEqual({ seen: 1, done: false });
  });

  // Симметричный случай: обрыв размонтированием раньше HINT_COUNT_MS не
  // засчитывается — порог не ослаблен, только добавлен путь, которым он
  // теперь применяется.
  //
  // Важная оговорка про то, что этот тест доказывает, а что нет. Он не
  // различает наличие/отсутствие самого крюка finishRef.current(false) в
  // cleanup-е размонтирования: до появления этого крюка размонтирование
  // тоже ничего не писало в хранилище (крюка не было — close() из cleanup-а
  // просто не вызывался), так что readHints()[...] был бы undefined что с
  // крюком, что без него — эмпирически проверено (временный откат крюка,
  // прогон, откат обратно). Различает этот тест другое, соседнее: что порог
  // HINT_COUNT_MS действительно применяется к пути размонтирования, а не
  // ослаблен или обойдён — если бы close() из cleanup-а звал markSeen
  // безусловно (например close(id, true) вместо счётного выражения),
  // именно этот тест поймал бы разницу (тоже проверено эмпирически). За
  // «крюк вообще есть» отвечает соседний тест выше («размонтирование после
  // достаточно долгого показа...») — там и до, и после появления крюка
  // поведение расходится по факту записи в хранилище.
  it('размонтирование до истечения HINT_COUNT_MS не засчитывает показ', () => {
    const { unmount } = setup();
    say(true);
    advance(HINT_APPEAR_MS + 100);
    expect(screen.getByRole('status')).toBeInTheDocument();
    unmount();
    expect(readHints()['page-numbers']).toBeUndefined();
  });

  // Панель чтения не размонтирует орган при прокрутке — только прячет его
  // якорь CSS'ом, из-за чего IntersectionObserver гасит выноску незасчитанным
  // close(id, false). Право экрана при этом не тратится, и без охраны на
  // уровне монтажа координатор перевыдавал бы его тому же id при каждом
  // возврате якоря — пузырёк открывался бы заново на каждой прокрутке вверх.
  it('после одного показа больше не открывается на этом монтаже, даже если координатор снова замкнулся на тот же id', () => {
    setup();
    say(true);
    advance(HINT_APPEAR_MS + 100);
    expect(screen.getByRole('status')).toBeInTheDocument();
    advance(HINT_COUNT_MS - 500);
    say(false);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    expect(readHints()['page-numbers']).toBeUndefined();

    // Якорь снова виден — координатор свободен и перевыдаёт право тому же id
    // (право экрана не потрачено), но этот монтаж уже показывал себя однажды.
    say(true);
    advance(HINT_APPEAR_MS + 100);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  // Регрессия: «взведено» хранилось голым булевым флагом на весь монтаж
  // компонента — верно для органов, которых координатор перемонтирует вместе
  // со сменой экрана, но не для карточки тома (WorkDetail.tsx) и соседних
  // глав (ChapterView.tsx), где сам орган намеренно переживает переход.
  // HintsProvider на смене pathname возвращает такому органу право экрана
  // заново, а флаг с булевым «уже было» навсегда запрещал бы второй показ —
  // притом что по договору у каждой выноски есть право до трёх показов НА
  // ЭКРАН, а не один на всю жизнь компонента.
  it('орган, переживший смену экрана, может показать выноску снова на новом экране', () => {
    setupPersistentAnchor();
    // Якорь остаётся видимым всё время — как на настоящей карточке тома,
    // если после перехода на соседний том читатель не успел прокрутить:
    // первый показ гаснет и засчитывается сам, автозакрытием, а не пропажей
    // якоря (см. «гаснет сама и засчитывает показ» выше).
    say(true);
    advance(HINT_APPEAR_MS + HINT_HIDE_MS + 100);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    expect(readHints()['outline-depth']).toEqual({ seen: 1, done: false });

    // Переход меняет pathname, но не размонтирует ни кнопку, ни FeatureHint —
    // оба стоят вне <Routes> в PersistentAnchorHarness, как и настоящий орган
    // на карточке тома.
    act(() => screen.getByText('дальше').click());

    advance(HINT_APPEAR_MS + 100);
    expect(screen.getByRole('status')).toHaveTextContent(HINTS['outline-depth'].text);
  });

  it('крестик закрывает подсказку навсегда', async () => {
    setup();
    say(true);
    advance(HINT_APPEAR_MS + 100);
    const close = screen.getByRole('button', { name: 'Больше не показывать' });
    act(() => close.click());
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    expect(readHints()['page-numbers']?.done).toBe(true);
  });

  it('нажатие самого органа усваивает подсказку, даже если её ещё не показали', () => {
    setup();
    act(() => screen.getByRole('button', { name: '№' }).click());
    expect(readHints()['page-numbers']?.done).toBe(true);
    say(true);
    advance(HINT_APPEAR_MS + 100);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('ведёт в справку по якорю своего идентификатора', () => {
    setup();
    say(true);
    advance(HINT_APPEAR_MS + 100);
    expect(screen.getByRole('link', { name: 'Подробнее' })).toHaveAttribute(
      'href',
      '/help#page-numbers',
    );
  });

  // Читатель, возможно, уже в тексте: выноска не диалог и фокус не забирает.
  it('не переносит фокус на себя', () => {
    setup();
    const anchor = screen.getByRole('button', { name: '№' });
    anchor.focus();
    say(true);
    advance(HINT_APPEAR_MS + 100);
    expect(document.activeElement).toBe(anchor);
  });

  // На экране чтения Escape уже делят выход из чтения (WorkRead) и подсказка
  // сноски (useFootnotePreview). Третий претендент сделал бы поведение одной
  // клавиши неугадываемым, поэтому выноска её не слушает вовсе.
  it('не перехватывает Escape', () => {
    setup();
    say(true);
    advance(HINT_APPEAR_MS + 100);
    act(() => {
      fireEvent.keyDown(document, { key: 'Escape' });
    });
    expect(screen.getByRole('status')).toBeInTheDocument();
  });

  // jsdom всегда отдаёт нулевой прямоугольник — чтобы получить решение
  // computePlacement, а не его текст, подменяем сам getBoundingClientRect.
  it('хвостик смотрит вниз, когда пузырёк вынужден встать над органом', () => {
    setup();
    const anchor = screen.getByRole('button', { name: '№' });
    anchor.getBoundingClientRect = () =>
      ({
        top: window.innerHeight - 20,
        bottom: window.innerHeight,
        left: 100,
        right: 140,
        width: 40,
        height: 20,
        x: 100,
        y: window.innerHeight - 20,
        toJSON: () => ({}),
      }) as DOMRect;
    say(true);
    advance(HINT_APPEAR_MS + 100);
    expect(screen.getByRole('status')).toHaveAttribute('data-placement', 'above');
  });

  it('хвостик смотрит вверх, когда пузырьку хватает места под органом', () => {
    setup();
    const anchor = screen.getByRole('button', { name: '№' });
    anchor.getBoundingClientRect = () =>
      ({
        top: 20,
        bottom: 40,
        left: 100,
        right: 140,
        width: 40,
        height: 20,
        x: 100,
        y: 20,
        toJSON: () => ({}),
      }) as DOMRect;
    say(true);
    advance(HINT_APPEAR_MS + 100);
    expect(screen.getByRole('status')).toHaveAttribute('data-placement', 'below');
  });

  it('вне провайдера не рисует ничего', () => {
    render(
      <MemoryRouter>
        <Harness />
      </MemoryRouter>,
    );
    say(true);
    advance(HINT_APPEAR_MS + 100);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });
});
