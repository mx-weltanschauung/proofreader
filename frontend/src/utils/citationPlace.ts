import type { Chapter, Work } from '../types';
import { chapterLevelsForPage } from '../hooks/useChaptersForPage';
import { authorsOf } from './credits';
import type { SignatureInput } from './citation';

/** Что подпись цитаты называет «произведением» и «автором» на данной полосе. */
export interface CitationPlace {
  workTitle: string;
  author: string;
}

/**
 * Статья номера журнала, накрывающая полосу: САМАЯ ГЛУБОКАЯ глава с видом
 * статьи (`article_kind`). Рубрика («Критика и библиография») — глава без
 * вида, и подписью статьи она не бывает: статья внутри неё глубже. На стыке
 * двух статей одного уровня берётся первая по порядку — тем же правилом, что
 * и у тома (`chapterLevelsForPage` сортирует уровень по `order_number`).
 */
export function articleAt(levels: Chapter[][]): Chapter | null {
  for (let depth = levels.length - 1; depth >= 0; depth--) {
    const article = levels[depth].find((c) => !!c.article_kind);
    if (article) return article;
  }
  return null;
}

/**
 * Произведение и автор подписи по уровням глав полосы — один помощник на все
 * поверхности цитаты (глава, поток, полоса со сканом, вклейка), иначе они
 * расходятся, как разошлись до финальной рецензии.
 *
 * Том: неаппаратная глава верхнего уровня и `work.author`. Номер журнала:
 * статья (`articleAt`) и её подпись (`authorsOf(credits)`); у номера
 * `work.author` пуст, а глава верхнего уровня бывает рубрикой. Полоса вне
 * всякой статьи (рубрика без статьи, служебная полоса) — пустые слоты, и
 * подпись остаётся подписью номера.
 */
export function citationPlaceFromLevels(work: Work, levels: Chapter[][]): CitationPlace {
  if (work.journal_issue) {
    const article = articleAt(levels);
    return article
      ? { workTitle: article.title, author: authorsOf(article.credits) }
      : { workTitle: '', author: '' };
  }
  return {
    workTitle: levels[0]?.find((c) => !c.is_apparatus)?.title ?? '',
    author: work.author ?? '',
  };
}

/**
 * То же по дереву глав работы. `within` — глава, которую читают
 * (`ChapterView`): у номера статья ищется сперва внутри неё, и только не
 * найдя — по всему дереву (раздел внутри статьи). Без этого на стыковой
 * полосе двух статей читатель второй получил бы подпись первой.
 */
export function citationPlace(
  work: Work,
  tree: Chapter[],
  pageNumber: number,
  within?: Chapter | null,
): CitationPlace {
  if (work.journal_issue && within) {
    const inside = articleAt(chapterLevelsForPage([within], pageNumber));
    if (inside) return { workTitle: inside.title, author: authorsOf(inside.credits) };
  }
  return citationPlaceFromLevels(work, chapterLevelsForPage(tree, pageNumber));
}

/** Журнальные координаты подписи; у тома — undefined. */
export function issueOf(work: Work): SignatureInput['issue'] {
  const ji = work.journal_issue;
  return ji ? { journal: ji.journal_title, year: ji.year, label: ji.label } : undefined;
}
