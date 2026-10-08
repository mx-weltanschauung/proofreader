import { useEffect } from 'react';
import { DEFAULT_SITE, useSite } from '../services/site';

/** Имя читальни по умолчанию — пока не пришли сведения экземпляра. */
export const SITE_NAME = DEFAULT_SITE.siteName;

/**
 * Ставит заголовок вкладки. До этого хука все страницы читальни назывались
 * одинаково — «Читальня», — и закладка на главу была неотличима от закладки
 * на том.
 *
 * Этот же заголовок увидит Гуглобот, когда всё-таки исполнит JS. Он обязан
 * совпадать с тем, что отдаёт краулеру internal/seo: расхождение читается
 * поисковиком как подмена содержимого.
 *
 * `null`/`undefined` означает «названия ещё нет, данные летят» — заголовок в
 * этом случае не трогается, иначе во вкладке мигнуло бы «undefined».
 */
export function useDocumentTitle(title: string | null | undefined): void {
  const { siteName } = useSite();
  useEffect(() => {
    if (!title) {
      return;
    }

    const previous = document.title;
    document.title = title.includes(siteName) ? title : `${title} — ${siteName}`;

    return () => {
      // Возвращаем то, что было: иначе уход со страницы главы оставил бы её
      // название на списке или на главной.
      document.title = previous;
    };
  }, [title, siteName]);
}
