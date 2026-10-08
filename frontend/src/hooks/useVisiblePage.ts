import { useCallback, useEffect, useRef, useState } from 'react';

// Полоса чтения: узкий пояс поперёк экрана вместо всего вьюпорта. Без него на
// странице главы «видимыми» одновременно оказывались бы страницы из разных
// подглав, и текущей приходилось бы считать любую из них.
const READING_BAND = '-30% 0px -65% 0px';

const ID_PREFIX = 'chapter-page-';
const SECTION_SELECTOR = `.chapter-page-section[id^="${ID_PREFIX}"]`;

function pageNumberOf(element: Element): number | null {
  const value = Number(element.id.slice(ID_PREFIX.length));
  return Number.isFinite(value) ? value : null;
}

/**
 * Как смена sectionsKey соотносится с прежним набором секций.
 *
 * `replace` — набор сменился целиком (чтение главы: ChapterView не
 * размонтируется при переходе, и прежняя видимая страница относится к уже
 * покинутой главе). `append` — секции только дописаны в конец (поток тома:
 * приехало следующее окно, а страница, на которую смотрит читатель, та же).
 */
export type SectionsChange = 'replace' | 'append';

/**
 * Номер верхней страницы, пересекающей полосу чтения, — то место, где читатель
 * сейчас находится.
 *
 * Секции хук находит в DOM сам, поэтому о их смене ему надо сказать: на смену
 * sectionsKey он перечитывает DOM.
 */
export function useVisiblePage(
  enabled: boolean,
  sectionsKey: unknown,
  change: SectionsChange = 'replace',
): number | null {
  const [page, setPage] = useState<number | null>(null);
  // sectionsKey — на практике массив объектов, склейка в строку различала бы
  // наборы только по длине. Сравниваем по ссылке, ровно как зависимость
  // эффекта ниже — это то же условие, под которым перечитывается DOM.
  const [prevKey, setPrevKey] = useState(sectionsKey);
  const [prevEnabled, setPrevEnabled] = useState(enabled);

  if (!Object.is(prevKey, sectionsKey) || prevEnabled !== enabled) {
    const onlyKeyChanged = prevEnabled === enabled;
    setPrevKey(sectionsKey);
    setPrevEnabled(enabled);
    // «Держим последнее значение» ниже — про прокрутку внутри одного набора
    // секций, а не про смену главы: ChapterView не размонтируется при
    // переходе, и без сброса окно до первого колбэка нового наблюдателя
    // показывало бы подглаву из уже покинутой главы.
    //
    // В режиме append сбрасывать, наоборот, нечего и вредно: секции лишь
    // дописаны в конец, прежняя видимая страница осталась правильной, а
    // обнуление на кадр-другой роняло бы в ноль всё, что за ней следит, —
    // полосу прогресса, бегущий заголовок главы, ссылку выхода.
    if (!(onlyKeyChanged && change === 'append')) setPage(null);
  }

  const observerRef = useRef<IntersectionObserver | null>(null);
  // Кого наблюдатель уже видел. Слабое множество: выбывшие из DOM секции
  // держать незачем, и сборщик их заберёт сам.
  const observedRef = useRef<WeakSet<Element>>(new WeakSet());

  const observeNewSections = useCallback(() => {
    const observer = observerRef.current;
    if (!observer) return;
    document.querySelectorAll(SECTION_SELECTOR).forEach((section) => {
      if (observedRef.current.has(section)) return;
      observedRef.current.add(section);
      observer.observe(section);
    });
  }, []);

  // Ключ жизни наблюдателя. В режиме append смена набора секций его НЕ
  // пересоздаёт: пересозданный регистрирует заново все накопленные секции —
  // ровно та квадратичность, ради ухода от которой поток и разбит на окна,
  // просто переехавшая из навешивания обработчиков в подписку наблюдателя.
  const observerKey = change === 'append' ? null : sectionsKey;

  useEffect(() => {
    if (!enabled) return;

    const visible = new Map<Element, number>();
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const number = pageNumberOf(entry.target);
          if (number === null) continue;
          if (entry.isIntersecting) visible.set(entry.target, number);
          else visible.delete(entry.target);
        }
        // Пусто на самом верху и в самом низу прокрутки. Держим последнее
        // известное значение: подсветка не должна мигать на краях.
        if (visible.size === 0) return;
        // Секции соседних страниц лежат рядом, и «верхняя» из пересекающих
        // полосу — наименьший номер. Но склейка полос вкладывает шов новой
        // страницы ВНУТРЬ секции предыдущей: там наименьший номер означал бы
        // предыдущую страницу на весь склеенный абзац. Объемлющие секции
        // поэтому отбрасываются, и «верхняя» ищется среди оставшихся.
        const targets = Array.from(visible.keys());
        const innermost = targets.filter(
          (element) => !targets.some((other) => other !== element && element.contains(other)),
        );
        setPage(Math.min(...innermost.map((element) => visible.get(element) as number)));
      },
      { rootMargin: READING_BAND },
    );
    observerRef.current = observer;
    observedRef.current = new WeakSet();
    observeNewSections();

    return () => {
      observer.disconnect();
      observerRef.current = null;
    };
  }, [enabled, observerKey, observeNewSections]);

  // Догрузка окна: подписываем только то, чего наблюдатель ещё не видел.
  // В режиме replace эффект выше уже подписал весь набор, и здесь ничего не
  // остаётся — эффекты идут по порядку объявления.
  useEffect(() => {
    if (!enabled) return;
    observeNewSections();
  }, [enabled, sectionsKey, observeNewSections]);

  return page;
}
