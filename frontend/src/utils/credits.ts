import type { ArticleCredit, ArticleKind } from '../types';

const byPosition = (a: ArticleCredit, b: ArticleCredit) => a.position - b.position;

/** Авторы статьи через запятую, по порядку подписи. */
export function authorsOf(credits: ArticleCredit[] | undefined): string {
  return (credits ?? [])
    .filter((c) => c.role === 'author')
    .sort(byPosition)
    .map((c) => c.printed)
    .join(', ');
}

/** Переводчики статьи через запятую. */
export function translatorsOf(credits: ArticleCredit[] | undefined): string {
  return (credits ?? [])
    .filter((c) => c.role === 'translator')
    .sort(byPosition)
    .map((c) => c.printed)
    .join(', ');
}

/** Метка вида статьи; у обычной статьи метки нет. */
export function articleKindLabel(kind: ArticleKind | '' | null | undefined): string {
  if (!kind || kind === 'статья') return '';
  return kind.replace('_', ' ');
}
