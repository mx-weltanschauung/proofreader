import React, { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Link } from 'react-router-dom';
import { useHints } from '../contexts/hintsContext';
import { HINTS, HINT_APPEAR_MS, HINT_COUNT_MS, HINT_HIDE_MS, type HintId } from '../hints/registry';
import { computePlacement } from '../hooks/popoverPlacement';
import { arrowOffset } from '../hints/arrowOffset';
import { useMeasuredBox } from '../utils/useMeasuredBox';
import './FeatureHint.css';

interface Props {
  id: HintId;
  /** Орган, который выноска объясняет. */
  anchorRef: React.RefObject<HTMLElement>;
}

/**
 * Пузырёк с хвостиком у кнопки, объясняющий, что она делает.
 *
 * Escape не слушает намеренно: на экране чтения его уже делят выход из чтения
 * (WorkRead) и подсказка сноски (useFootnotePreview), и третий претендент
 * сделал бы поведение одной клавиши неугадываемым. Выноска гаснет сама и
 * закрывается крестиком.
 */
export const FeatureHint: React.FC<Props> = ({ id, anchorRef }) => {
  const hints = useHints();
  // Разложено по простым именам, чтобы списки зависимостей эффектов не
  // пересчитывались на каждый рендер провайдера: сам объект контекста меняется
  // вместе с activeId, а функции в нём стабильны (useCallback).
  const register = hints ? hints.register : null;
  const unregister = hints ? hints.unregister : null;
  const setVisible = hints ? hints.setVisible : null;
  const learn = hints ? hints.learn : null;
  const close = hints ? hints.close : null;
  const markOpened = hints ? hints.markOpened : null;
  const screen = hints ? hints.screen : null;

  // Свой номер экземпляра. Орган бывает смонтирован дважды (кнопка «Aa» — в
  // шапке сайта и в панели чтения), и право показаться выдаётся не
  // идентификатору, а конкретной копии — той, чей якорь виден. Без этого обе
  // копии считали бы право своим, и та, что скрыта, рисовала бы пузырёк с
  // нулевым прямоугольником в углу экрана.
  const token = useId();
  const active = hints ? hints.active : null;
  const isActive = active?.id === id && active.token === token;

  const [open, setOpen] = useState(false);
  // Зеркало open вне рендера. Таймер автозакрытия планируется в том же
  // обратном вызове setTimeout, что и появление (см. эффект ниже, отвечающий
  // за появление и автозакрытие), то есть раньше, чем React вообще успевает
  // закоммитить setOpen(true) — обычный `open` из состояния к этому моменту
  // ещё врёт. finish() поэтому решает «было ли что закрывать» по этому ref,
  // а не по состоянию.
  const openRef = useRef(false);
  // Крюк замерщика (frontend/scripts/measure-feature-hint.mjs): кладёт
  // прямоугольник пузырька и ширину окна в data-hint-box, поведение ref не
  // меняется.
  const bubbleRef = useMeasuredBox<HTMLDivElement>('data-hint-box');
  const shownAtRef = useRef(0);
  // Экран, на котором этот орган уже показал себя однажды. Координатор
  // может замкнуться на этот id повторно в рамках одного и того же экрана:
  // close(id, false) — незасчитанный обрыв — снимает свой замок, но не
  // трогает spent, и если якорь тут же снова становится видимым (панель
  // чтения гаснет и возвращается при прокрутке туда-обратно), candidate тем
  // же тиком выбирает тот же id заново. Без охраны выноска открывалась бы
  // при каждом таком цикле.
  //
  // Хранит именно ЗНАЧЕНИЕ экрана (hints.screen), а не голый булев флаг «уже
  // взведено» на весь монтаж компонента: часть органов переживает смену
  // экрана, не размонтируясь (WorkDetail.tsx — переход между томами по
  // ссылке «Предваряющие материалы», ChapterView.tsx — переход между
  // соседними главами: тот же элемент маршрута, react-router не
  // перемонтирует компонент при смене только параметра). Координатор
  // возвращает такому органу право экрана на новом экране, но с голым
  // булевым флагом эта выноска никогда не показалась бы во второй раз за
  // всю жизнь компонента — а по договору у каждой полагается до трёх
  // показов НА ЭКРАН. Сравнение с hints.screen — тем же понятием «новый
  // экран», которым уже пользуется сам координатор (см. screen в
  // hintsContext.ts) — снимает охрану ровно тогда, когда экран сменился, и
  // не раньше. Не заменяет и не ослабляет порог HINT_COUNT_MS в close() —
  // тот по-прежнему решает, засчитан ли показ в лимит из трёх; это — только
  // про «не больше одного показа за один и тот же экран».
  const armedScreenRef = useRef<string | null>(null);

  // Подхватить рассинхронизацию openRef из путей, где open меняют не через
  // openRef напрямую (например снаружи — координатор вправе сбросить замок
  // без ведома выноски). Сам по себе не участвует в тех же тик-чувствительных
  // гонках, что и внутренний таймер: событие, которое меняет open таким
  // образом, — отдельный, более поздний тик, эффект успевает отработать.
  useEffect(() => {
    openRef.current = open;
  }, [open]);

  const finish = useCallback(
    (learned: boolean) => {
      const wasOpen = openRef.current;
      if (wasOpen) {
        openRef.current = false;
        setOpen(false);
      }
      if (learned) {
        // learn помечает подсказку усвоенной безусловно, даже если координатор
        // её ни разу не запирал (клик по органу раньше первого показа) — это
        // факт про саму подсказку, а не про право экрана, и HintsProvider
        // сам решает, когда ещё и погасить активную выноску (см. его learn).
        learn?.(id);
        return;
      }
      if (!wasOpen) return;
      close?.(id, Date.now() - shownAtRef.current >= HINT_COUNT_MS);
    },
    [id, learn, close],
  );

  // Callback в ref: наблюдатель и слушатель клика ставятся один раз, а звать
  // обязаны свежую версию. Тот же приём, что в useScrollDockAction.
  const finishRef = useRef(finish);
  useEffect(() => {
    finishRef.current = finish;
  });

  // Регистрация, наблюдение за якорем и перехват нажатия по органу.
  useEffect(() => {
    if (!register || !unregister || !setVisible) return;
    register(id, token);
    const node = anchorRef.current;
    if (!node) return () => unregister(id, token);

    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries[entries.length - 1]?.isIntersecting ?? false;
        if (!visible) finishRef.current(false);
        setVisible(id, token, visible);
      },
      { threshold: 0.5 },
    );
    observer.observe(node);

    // Читатель нашёл орган сам — объяснять больше нечего, в том числе если
    // выноску ему ещё ни разу не показали.
    const onUse = () => finishRef.current(true);
    node.addEventListener('click', onUse);

    return () => {
      observer.disconnect();
      node.removeEventListener('click', onUse);
      // Размонтирование орган уносит с собой (ScrollDock прячет кнопку
      // возвратом null, а не CSS) — координатор без этого вызова никогда не
      // узнал бы, чем закончился показ: close() не звонили явно, spent
      // остаётся false, и следующий такой же орган (новый монтаж после
      // прокрутки обратно) получает право показаться заново, хотя выноска
      // уже провисела своё. finish(false) сам решает по openRef/shownAtRef,
      // было ли что закрывать и достаточно ли это провисело — тот же путь,
      // что у обычного закрытия по исчезновению якоря, порог HINT_COUNT_MS
      // не меняется.
      finishRef.current(false);
      unregister(id, token);
    };
  }, [register, unregister, setVisible, id, token, anchorRef]);

  // Право показаться выдано — ждём, пока экран устоится, потом показываем и
  // сразу же ставим автозакрытие.
  //
  // Раньше здесь стоял синхронный setOpen(false) прямо в теле этого эффекта
  // на смену activeId («уехал — закрыть»), что запрещено
  // react-hooks/set-state-in-effect: это правило против эффектов, которые
  // просто зеркалят входное значение в своё состояние (производное
  // состояние, которое React просит считать во время рендера, а не через
  // цикл commit → effect → re-render). Официальный приём — «Adjusting state
  // when a prop changes»: сравнить activeId с прошлым рендером прямо в теле
  // компонента (см. ниже, тот же приём, что prevPathname в HintsProvider).
  // Ref для «предыдущего значения» тут не подходит: react-hooks/refs отдельно
  // запрещает мутировать ref во время рендера, а не только в эффекте.
  //
  // Автозакрытие — таймер внутри ТОГО ЖЕ обратного вызова, что открытие, а
  // не отдельный эффект на `open`: при быстрой перемотке таймеров в тестах
  // (`vi.advanceTimersByTime` одним большим прыжком) React батчит setOpen(true)
  // до конца всего прыжка, и эффект, реагирующий на `open`, узнал бы о нём,
  // когда виртуальные часы уже дальше нужной отметки — секундный таймер
  // регистрировался бы «в прошлом» и никогда не сработал бы вовремя. Обычный
  // setTimeout внутри уже идущего обратного вызова планируется относительно
  // настоящего виртуального «сейчас» и потому fake-timers ловят его верно.
  useEffect(() => {
    if (!isActive) return;
    // Этот экран уже показывал выноску однажды — второго раза не будет, даже
    // если координатор выдал право экрана заново (см. комментарий у
    // armedScreenRef). screen — в зависимостях эффекта намеренно: без этого
    // эффект не перезапустился бы при смене экрана у органа, что пережил её
    // без размонтирования, если activeId в терминальном рендере совпал с тем
    // же значением, что и до смены (координатор может успеть сбросить и тут
    // же заново выдать locked за один и тот же цикл рендеров, и снаружи
    // FeatureHint это выглядит так, будто activeId вовсе не менялся).
    if (armedScreenRef.current === screen) return;
    let hideTimer: number | undefined;
    const appearTimer = window.setTimeout(() => {
      armedScreenRef.current = screen;
      openRef.current = true;
      // Замок теперь неприкосновенен: до этого мгновения координатор был
      // вправе передать его подсказке поважнее, чей якорь подъехал позже
      // (см. markOpened в hintsContext.ts).
      markOpened?.(id, token);
      setOpen(true);
      shownAtRef.current = Date.now();
      hideTimer = window.setTimeout(() => finishRef.current(false), HINT_HIDE_MS);
    }, HINT_APPEAR_MS);
    return () => {
      window.clearTimeout(appearTimer);
      window.clearTimeout(hideTimer);
    };
  }, [isActive, screen, markOpened, id, token]);

  const [prevActive, setPrevActive] = useState(isActive);
  if (prevActive !== isActive) {
    setPrevActive(isActive);
    if (!isActive) setOpen(false);
  }

  // Раскладка. Считает готовая computePlacement — та же, что у подсказок
  // сносок; пузырьку остаётся только хвостик.
  useLayoutEffect(() => {
    const bubble = bubbleRef.current;
    const anchor = anchorRef.current;
    if (!open || !bubble || !anchor) return;

    const place = () => {
      const rect = anchor.getBoundingClientRect();
      const { top, left, placement } = computePlacement(
        { top: rect.top, bottom: rect.bottom, left: rect.left },
        { width: bubble.offsetWidth, height: bubble.scrollHeight },
        { width: window.innerWidth, height: window.innerHeight },
      );
      bubble.style.top = `${window.scrollY + top}px`;
      bubble.style.left = `${window.scrollX + left}px`;
      bubble.style.setProperty(
        '--hint-arrow-x',
        `${arrowOffset(rect.left + rect.width / 2, left, bubble.offsetWidth)}px`,
      );
      // Атрибут, а не класс: это данные о выбранной стороне, а не имя стиля,
      // которое пришлось бы согласовывать с проверкой имён btn-*/классов.
      // Якорь у нижнего края экрана (например, кнопка возврата к подглаве —
      // ScrollDock прижат книзу) обычно получает 'above', и хвостик обязан
      // развернуться на нижнюю кромку пузырька, а не торчать вверх мимо кнопки.
      bubble.dataset.placement = placement;
    };

    place();
    window.addEventListener('resize', place);
    window.addEventListener('scroll', place, { passive: true });
    return () => {
      window.removeEventListener('resize', place);
      window.removeEventListener('scroll', place);
    };
    // bubbleRef — не голый useRef, а объект из useMeasuredBox (стабилен, пока
    // стабильно имя атрибута), поэтому линтер не выводит его стабильность сам
    // и просит явную зависимость.
  }, [open, anchorRef, bubbleRef]);

  if (!open) return null;

  return createPortal(
    // data-hint-id — не для стиля, а для замерщика вёрстки
    // (frontend/scripts/measure-feature-hint.mjs): document.querySelector по
    // одному лишь [data-hint-box] берёт первый попавшийся пузырёк на
    // странице, не разбирая, чей он — на карточке тома так однажды измерили
    // «Aa» вместо глубины содержания. Явный id даёт скрипту проверить, что
    // измерен тот орган, который ожидался.
    <div className="feature-hint" ref={bubbleRef} role="status" data-hint-id={id}>
      <span className="feature-hint-arrow" aria-hidden="true" />
      <p className="feature-hint-text">{HINTS[id].text}</p>
      <div className="feature-hint-foot">
        <Link className="feature-hint-more" to={`/help#${id}`} onClick={() => finish(true)}>
          Подробнее
        </Link>
        <button
          type="button"
          className="feature-hint-close"
          aria-label="Больше не показывать"
          onClick={() => finish(true)}
        >
          <span aria-hidden="true">✕</span>
        </button>
      </div>
    </div>,
    document.body,
  );
};
