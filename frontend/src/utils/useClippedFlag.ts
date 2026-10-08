import { useEffect, useRef } from 'react';

/**
 * Помечает элемент, чей текст не поместился в отведённую ему ширину: ставит
 * `data-clipped` (по нему стиль показывает обрыв) и кладёт сам замер в
 * `data-fit` — `scrollWidth,clientWidth`, целыми пикселями, крюк для
 * замерщика вёрстки (`frontend/scripts/measure-volume-shelf.mjs`).
 *
 * Замером, а не длиной строки: ширина знака у Literata своя у каждой буквы, и
 * заглавие в 61 знак может не поместиться туда, куда влезает другое в 92.
 * Порог по числу знаков поэтому врёт в обе стороны, и «влезло» знает только
 * движок. (Пока книги стояли, врал он ещё сильнее: подпись набиралась
 * вертикально и переносилась по колонкам рваным переносом.)
 *
 * Ждём шрифты: Literata шире запасной Georgia, и замер до её загрузки
 * объявил бы поместившимся то, что потом обрежется. Родня useMeasuredHeight —
 * там та же оговорка про document.fonts, здесь тот же однократный замер:
 * замерщик открывает страницу заново, а без списка зависимостей эффект
 * переписывал бы атрибут на каждый рендер штабеля.
 */
export function useClippedFlag<T extends HTMLElement>(deps: unknown = null) {
  const ref = useRef<T>(null);

  useEffect(() => {
    let cancelled = false;
    const measure = () => {
      const node = ref.current;
      if (cancelled || !node) return;
      const scroll = node.scrollWidth;
      const client = node.clientWidth;
      // Запись отложена: в штабеле крюк висит на сорока пяти книгах разом, и
      // атрибут, поставленный сразу, помечал бы раскладку грязной — чтение
      // ширины следующей книги пересчитывало бы её заново, и так сорок пять
      // раз. Читают все в одном проходе (микрозадачи от одного
      // document.fonts.ready), пишут все в следующем.
      //
      // Откладывать надо задачей, а НЕ requestAnimationFrame, хотя кадр здесь
      // просится сам: headless Chrome под --virtual-time-budget кадры выдаёт
      // не всегда, и rAF-колбэк до дампа не доживал. Замер по всем сорока
      // пяти книгам получался «всё или ничего» — три прогона подряд
      // (`--dump-dom`) давали 0 записей, четвёртый 45. Сторож вёрстки
      // (scripts/measure-volume-shelf.mjs) читает как раз этот атрибут, то
      // есть проходил по удаче; с setTimeout три прогона из трёх дают 45.
      // Пакетность от замены не страдает: чтения всё так же идут до первой
      // записи, а data-clipped правит только маску, то есть отрисовку, и
      // ждать кадра ему незачем.
      setTimeout(() => {
        if (cancelled) return;
        node.setAttribute('data-fit', `${scroll},${client}`);
        if (scroll > client) node.setAttribute('data-clipped', '');
        else node.removeAttribute('data-clipped');
      });
    };

    if (document.fonts) {
      // Второй аргумент .then — обработчик отказа: .catch(measure) следом
      // выполнил бы measure и при исключении из самогó measure.
      void document.fonts.ready.then(measure, measure);
    } else {
      measure();
    }

    return () => {
      cancelled = true;
    };
    // Заглавие у книги своё и не меняется, но штабель переиспользует узлы при
    // смене собрания — тогда замер нужен заново.
  }, [deps]);

  return ref;
}
