import { useEffect, useRef, useState } from 'react';
import { markQuote, type QuoteMatch, type QuoteSpan } from '../utils/quoteMatch';
import { scrollBehavior } from '../utils/motion';

/**
 * Снимает разметку прежних совпадений перед новым поиском.
 *
 * Без этого повторный вызов эффекта БЕЗ замены innerHTML (пример: тот же
 * контент главы, но `pageNumber` пересчитался из хэша адреса) находит старый
 * `<mark>` уже внутри дерева — `flattenText` не отсеивает `.quote-hit`
 * нарочно, тот же узел, что и обычный текст, — и второй `markQuote` либо
 * оставляет чужую подсветку рядом с новой, либо, при точном совпадении границ,
 * оборачивает уже обёрнутое. Замены innerHTML (новая полоса, новый `?quote=`)
 * это не касаются — там прежних `<mark>` в дереве попросту нет.
 */
function unmarkQuoteHits(root: HTMLElement): void {
  for (const mark of Array.from(root.querySelectorAll('mark.quote-hit'))) {
    const parent = mark.parentNode;
    if (!parent) continue;
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark);
    parent.removeChild(mark);
    parent.normalize();
  }
}

/**
 * Приём цитаты из `?quote=`: подсветка в отрисованном html и исход сравнения
 * для QuoteNotice. По образцу useSearchHighlight — перезапуск по `deps`
 * потребителя (новый innerHTML стирает прежние `<mark>`), прокрутка к первому
 * совпадению один раз за жизнь компонента. Пустая цитата — ни обхода DOM, ни
 * состояния: `null`, и вызывающая сторона не рисует QuoteNotice вовсе.
 *
 * `pageNumber` — не украшение, а условие правильности: головная цитата
 * вырезки указателя (задача 14) — ключ, уникальный на СВОЕЙ полосе
 * (quoteKey.ts), а не по всей главе. Без сужения то же сочетание слов,
 * встретившееся ещё раз на другой полосе многостраничной главы, законно дало
 * бы 'multiple' и подсветило бы чужое место — притом что адрес, с которого
 * пришла ссылка, известен заранее и полосу называет точно.
 *
 * Без номера полосы (`pageNumber === undefined`) поиск НЕ идёт вовсе — это
 * не декорация и не смягчение до поиска по всей главе. Адрес, не назвавший
 * места, ничего не подтверждает: подсветка первого совпадения в главе до 742
 * полос завела бы читателя не туда молча — хуже честного бездействия. Все
 * адреса, которые эта ссылка умеет производить (CiteButton, задача 14),
 * всегда несут номер полосы; читатель, свернувший чужой адрес руками, увидит
 * просто нетронутый текст, а не ложное попадание.
 */
export function useQuoteHighlight(
  containerRef: React.RefObject<HTMLElement>,
  span: QuoteSpan,
  pageNumber: number | undefined,
  deps: unknown[],
): QuoteMatch | null {
  // Зависимости берутся ПОЛЯМИ пары, а не самой парой: вызывающие собирают
  // её на каждый рендер заново ({ start, end } из двух параметров адреса), и
  // зависимость по объекту перезапускала бы эффект всегда — то есть гоняла бы
  // flattenText по всей главе (366 мс на 742 полосах) на каждый рендер
  // читалки.
  const { start, end } = span;
  // Посадка на найденное место — прокрутка И фокус — делается один раз за
  // жизнь компонента: читатель, ушедший фокусом на кнопку панели, не должен
  // получать его обратно на каждую перерисовку читалки.
  const landed = useRef(false);
  const [match, setMatch] = useState<QuoteMatch | null>(null);

  useEffect(() => {
    const root = containerRef.current;
    if (!root) {
      setMatch(null);
      return;
    }

    unmarkQuoteHits(root);

    if (start === '' || pageNumber === undefined) {
      // Сброс — прямой отклик на снятую разметку строкой выше, а не
      // синхронизация с пропом: перенести в тело хука нельзя, снятие
      // разметки — мутация чужого DOM, ей место только в эффекте.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setMatch(null);
      return;
    }

    const result = markQuote(root, { start, end }, pageNumber);
    setMatch(result);

    // `result !== 'miss'` здесь — защита про запас, не рабочая ветка: markQuote
    // не оборачивает ни одного узла в 'miss' (см. quoteMatch.ts), поэтому
    // querySelector('mark.quote-hit') и без этого условия вернёт null и `?.`
    // погасит вызов сам. Мутация, снимающая только это условие, ни один тест
    // не красит по этой же причине — честно называем это здесь, а не
    // притворяемся, что мутация где-то поймана.
    if (result !== 'miss' && !landed.current) {
      landed.current = true;
      const hit = root.querySelector('mark.quote-hit');
      if (hit instanceof HTMLElement) {
        // Фокус переезжает СЮДА с якоря полосы, куда его только что поставил
        // useHashAnchor (jumpToAnchor): якорь называет полосу, ссылка
        // называет место. Порядок держится порядком хуков в ReadingSurface —
        // useHashAnchor объявлен выше, поэтому его эффект отрабатывает
        // раньше и последнее слово остаётся за цитатой.
        //
        // Без этого переезда читатель, пришедший по ссылке, видел кольцо
        // `:focus-visible` вокруг соседнего куска текста ВЫШЕ цитаты: у
        // склеенной полосы якорь стоит на шве — строчном span посреди чужого
        // абзаца, — и обводка ложилась на конец предыдущей страницы.
        //
        // tabIndex ставится здесь, а не разметкой: <mark> порождает markQuote
        // на лету, своего узла в JSX у него нет.
        hit.tabIndex = -1;
        // preventScroll — ради ОДНОГО движения вместо двух: без него focus()
        // сперва мгновенно подтягивает подсветку к краю окна, и только потом
        // идёт плавная прокрутка строкой ниже, то есть читатель видит рывок
        // и доводку. Замер на живом томе (глава 25560, полоса 370, окно 857):
        // с preventScroll цитата встаёт в 40 px от центра, без него — в 1 px,
        // разница в 39 px — цена scroll-padding под липкой шапкой, а не
        // отменённого block: 'center'. Плата за одно движение, и её видно.
        hit.focus({ preventScroll: true });
        hit.scrollIntoView({ block: 'center', behavior: scrollBehavior() });
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [containerRef, start, end, pageNumber, ...deps]);

  return match;
}
