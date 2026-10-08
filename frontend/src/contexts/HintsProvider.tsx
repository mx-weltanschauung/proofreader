import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';
import { HINTS, type HintId } from '../hints/registry';
import {
  isLearned,
  markDone,
  markSeen,
  readHints,
  writeHints,
  type HintsState,
} from '../hints/hintsStorage';
import { HintsContext, type HintInstance } from './hintsContext';

/**
 * Смонтированные выноски: идентификатор -> экземпляр -> виден ли его якорь.
 *
 * Два уровня, а не один, потому что орган может быть смонтирован не один раз
 * (кнопка «Aa» — в шапке сайта и в панели чтения). Прежде видимость хранилась
 * одним слотом на идентификатор, и копии дрались за него: та, что
 * размонтировалась, стирала видимость оставшейся, а право показаться
 * получали обе разом — включая скрытую, чей пузырёк рисовался в углу экрана
 * с хвостиком в никуда.
 */
type Anchors = Partial<Record<HintId, Record<string, boolean>>>;

export const HintsProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [state, setState] = useState<HintsState>(readHints);
  /**
   * Зеркало state для записи в хранилище.
   *
   * Раньше writeHints звался внутри обновления состояния — и это молча
   * переставало работать ровно там, где нужнее всего: при размонтировании
   * (кнопка подглавы исчезает вместе с блоком навигации) React отбрасывает
   * обновление ещё до того, как позовёт функцию-обновитель, а вместе с ним
   * пропадала и запись. Показ, который читатель видел, не засчитывался.
   * Хранилище — не производная состояния React, поэтому пишется прямо, а
   * setState остаётся только про перерисовку: не доживёт до коммита — не беда.
   */
  const stateRef = useRef(state);
  const [anchors, setAnchors] = useState<Anchors>({});
  /** Кому уже выдано право показаться. Замок держится до закрытия выноски. */
  const [locked, setLocked] = useState<HintInstance | null>(null);
  /** Право экрана израсходовано: показ состоялся и был засчитан. */
  const [spent, setSpent] = useState(false);
  /** Замкнутый пузырёк уже на экране — перехватывать замок поздно. */
  const [opened, setOpened] = useState(false);
  const { pathname } = useLocation();

  // Свежий locked для learn/close: у них useCallback с пустыми зависимостями,
  // поэтому обычное замыкание держало бы значение locked с первого рендера.
  // Ref сам по себе не участвует в рендере, поэтому обновлять его — законная
  // работа эффекта (в отличие от setState, которого правило
  // react-hooks/set-state-in-effect и запрещает); мутировать его прямо в теле
  // компонента, во время рендера, react-hooks/refs запрещает отдельно.
  const lockedRef = useRef(locked);
  useEffect(() => {
    lockedRef.current = locked;
  }, [locked]);

  // Новый экран — новое право на одну выноску. Сброс делается прямо во время
  // рендера, а не в useEffect: это официальный приём React «подстроить
  // состояние под смену пропса» (см. «Adjusting state when a prop changes» в
  // документации useState) — правило react-hooks/set-state-in-effect как раз
  // запрещает синхронный setState внутри тела эффекта, потому что для этого
  // случая рендер-время дешевле лишнего цикла commit → effect → re-render.
  const [prevPathname, setPrevPathname] = useState(pathname);
  if (prevPathname !== pathname) {
    setPrevPathname(pathname);
    setSpent(false);
    setLocked(null);
    setOpened(false);
  }

  // Дубликат id больше не беда и не требует охраны: экземпляры считаются
  // порознь, а право показаться получает один из них — тот, чей якорь виден.
  const register = useCallback((id: HintId, token: string) => {
    setAnchors((prev) => {
      if (prev[id] && token in prev[id]) return prev;
      return { ...prev, [id]: { ...prev[id], [token]: false } };
    });
  }, []);

  const unregister = useCallback((id: HintId, token: string) => {
    setAnchors((prev) => {
      const instances = prev[id];
      if (!instances || !(token in instances)) return prev;
      const rest = { ...instances };
      delete rest[token];
      const next = { ...prev };
      // Последний экземпляр ушёл — убираем и сам идентификатор, чтобы он не
      // висел пустой записью в кандидатах.
      if (Object.keys(rest).length === 0) delete next[id];
      else next[id] = rest;
      return next;
    });
  }, []);

  const setVisible = useCallback((id: HintId, token: string, visible: boolean) => {
    setAnchors((prev) => {
      const instances = prev[id];
      if (!instances || instances[token] === visible) return prev;
      return { ...prev, [id]: { ...instances, [token]: visible } };
    });
  }, []);

  // Пузырёк показался — замок больше не перехватить (см. markOpened в
  // hintsContext.ts). Сверяется с замкнутым экземпляром: сообщение от копии,
  // которой право не выдавали, ничего не значит.
  const markOpened = useCallback((id: HintId, token: string) => {
    if (lockedRef.current?.id !== id || lockedRef.current.token !== token) return;
    setOpened(true);
  }, []);

  // Запись в хранилище внутри обновления состояния — тот же приём, что в
  // ReadingPreferencesContext: повторный вызов под StrictMode идемпотентен.
  /** Записать новое состояние подсказок: в хранилище сразу, в рендер — как выйдет. */
  const persist = useCallback((next: HintsState) => {
    stateRef.current = next;
    writeHints(next);
    setState(next);
  }, []);

  const learn = useCallback(
    (id: HintId) => {
      // Усвоение принадлежит подсказке, а не экрану: «читатель нашёл орган сам»
      // истинно независимо от того, выдавал ли координатор этой подсказке
      // право показаться. Поэтому markDone — безусловно, для любого id. А вот
      // право экрана и снятие замка — общий, разделяемый на весь экран ресурс:
      // их трогать можно, только если id — та самая подсказка, что его сейчас
      // держит (сверяется с lockedRef, а не с locked из замыкания). Иначе клик
      // по органу, до которого координатор ещё не дошёл, мог бы погасить
      // действительно показанную выноску или списать чужое право экрана.
      persist(markDone(stateRef.current, id));
      if (lockedRef.current?.id !== id) return;
      setLocked((prev) => (prev?.id === id ? null : prev));
      setOpened(false);
      setSpent(true);
    },
    [persist],
  );

  const close = useCallback(
    (id: HintId, counted: boolean) => {
      // В отличие от learn, здесь охрана целиком: close описывает не факт про
      // подсказку, а исход её собственного показа («сколько провисела, пока
      // гасла»), и это утверждение бессмысленно для подсказки, которую сейчас
      // не показывают — закрыть можно только замкнутую.
      if (lockedRef.current?.id !== id) return;
      if (counted) {
        persist(markSeen(stateRef.current, id));
        setSpent(true);
      }
      setLocked((prev) => (prev?.id === id ? null : prev));
      setOpened(false);
    },
    [persist],
  );

  // Сброс координатора для повторного показа («показать подсказки заново» на
  // странице справки). localStorage провайдер читает один раз, в
  // инициализаторе useState — очистка ключа снаружи не заставит его
  // перечитаться сама по себе, поэтому здесь состояние сбрасывается в памяти
  // напрямую. Само хранилище reset() не трогает: очисткой ключа занимается
  // вызывающая сторона, и её тест проверяет именно это.
  const reset = useCallback(() => {
    stateRef.current = {};
    setState({});
    setSpent(false);
    setLocked(null);
    setOpened(false);
  }, []);

  /**
   * Кого стоило бы показать, если бы право было свободно.
   *
   * Идентификатор годится в кандидаты, если виден хоть один его экземпляр;
   * право получает именно этот видимый экземпляр. Порядок токенов — порядок
   * регистрации, то есть выбор устойчив: при двух одновременно видимых
   * копиях (в живом интерфейсе такого нет, но модель это допускает) каждый
   * рендер выберет одну и ту же.
   */
  const candidate = useMemo<HintInstance | null>(() => {
    const eligible = (Object.keys(anchors) as HintId[])
      .filter((id) => !isLearned(state, id))
      .map((id) => ({
        id,
        token: Object.keys(anchors[id] ?? {}).find((t) => anchors[id]?.[t]) ?? null,
      }))
      .filter((entry): entry is HintInstance => entry.token !== null);
    eligible.sort((a, b) => HINTS[b.id].priority - HINTS[a.id].priority);
    return eligible[0] ?? null;
  }, [anchors, state]);

  // Кому достаётся право показаться.
  //
  // Свободный замок берёт текущий лучший кандидат. Занятый — можно перехватить,
  // но только пока пузырёк ещё не на экране (opened) и только строго более
  // приоритетным: у читателя ничего не меняется на глазах, а вот очерёдность
  // перестаёт зависеть от гонки загрузки. Без перехвата приоритеты не работали
  // вовсе: якорь «Aa» живёт в шапке сайта и виден с первого кадра, а
  // собственные органы экрана — оглавление, обрез, скачивание — появляются
  // только после ответа API, и замок всегда доставался шапке.
  //
  // Приём рендер-времени, как и сброс по pathname выше: условие само себя
  // гасит на следующем рендере (замок становится тем самым кандидатом, и
  // «строго приоритетнее» больше не выполняется), цикла не возникает.
  const better =
    candidate !== null &&
    (locked === null || (!opened && HINTS[candidate.id].priority > HINTS[locked.id].priority));
  if (!spent && better) {
    setLocked(candidate);
  }

  // Якорь замкнутого экземпляра исчез (или подсказку успели усвоить) — замок
  // снимается, право экрана при этом не тратится: показа не было. Проверяется
  // именно этот экземпляр: другая копия того же органа могла остаться видимой,
  // но замок принадлежит не ей.
  if (
    locked !== null &&
    (anchors[locked.id]?.[locked.token] !== true || isLearned(state, locked.id))
  ) {
    setLocked(null);
  }

  const value = useMemo(
    () => ({
      active: spent ? null : locked,
      // Отдано наружу для FeatureHint — см. комментарий у screen в
      // hintsContext.ts: своё «взведено ли уже» орган обязан сверять с тем
      // же pathname, которым здесь определяется новый экран, а не жить всё
      // время жизни компонента — часть якорей (карточка тома, соседняя
      // глава) переживает смену экрана, не размонтируясь.
      screen: pathname,
      register,
      unregister,
      setVisible,
      markOpened,
      learn,
      close,
      reset,
    }),
    [spent, locked, pathname, register, unregister, setVisible, markOpened, learn, close, reset],
  );

  return <HintsContext.Provider value={value}>{children}</HintsContext.Provider>;
};
