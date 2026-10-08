import { useEffect, useRef } from 'react';
import { highlightTerms } from '../utils/searchHighlight';
import { scrollBehavior } from '../utils/motion';

/**
 * Подсветка найденного в отрисованном html. Как и остальные хуки читалки
 * (useKatex, usePageSeams), вешается на контейнер и перезапускается по deps
 * потребителя: новый innerHTML стирает прежние <mark>. Первое совпадение
 * прокручивается в кадр один раз за жизнь компонента.
 */
export function useSearchHighlight(
  containerRef: React.RefObject<HTMLElement>,
  terms: string[],
  scrollToFirst: boolean,
  deps: unknown[],
): void {
  const key = terms.join(' ');
  const scrolled = useRef(false);

  useEffect(() => {
    const root = containerRef.current;
    if (!root || key === '') return;
    const stems = key.split(' ');
    // Обход ограничен .page-html-content, а не всем контейнером: рядом с ним
    // в том же контейнере лежит .page-marker — голый номер страницы, а
    // регулярка слова матчит и цифры, так что числовой запрос подсвечивал бы
    // номера страниц. .page-html-content'ы идут в контейнере в порядке
    // документа, поэтому первый найденный <mark> в контейнере после обхода
    // всех них — по-прежнему первый по тексту, и запрос ниже за ним не
    // теряет точность.
    // Контейнер бывает двух видов: у читалки он ОБЁРТКА над несколькими
    // .page-html-content, у одиночной полосы (PageView) он сам и есть
    // .page-html-content. querySelectorAll себя не находит, поэтому корень,
    // подходящий под селектор, добавляется отдельно — иначе на полосе,
    // открытой из выдачи, не подсветилось бы ничего.
    let found = 0;
    const blocks = root.matches('.page-html-content')
      ? [root]
      : [...root.querySelectorAll<HTMLElement>('.page-html-content')];
    for (const block of blocks) {
      found += highlightTerms(block, stems);
    }
    if (found > 0 && scrollToFirst && !scrolled.current) {
      scrolled.current = true;
      root
        .querySelector('mark.search-hit')
        ?.scrollIntoView({ block: 'center', behavior: scrollBehavior() });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [containerRef, key, scrollToFirst, ...deps]);
}
