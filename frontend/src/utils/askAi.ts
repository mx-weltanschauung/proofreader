import type { Chapter, Concept, Work } from '../types';
import { chapterPath } from './paths';
import { printedFolio } from './folio';
import { encodeRubricPath } from './rubricPathParam';
import { shownVolumeNumber, shownVolumePart } from './shownVolume';

/** Как pkg/book/title.go: «. » между частями, но не после знака конца. */
function joinSourceParts(parts: (string | undefined)[]): string {
  let out = '';
  for (const raw of parts) {
    const part = (raw ?? '').trim();
    if (!part) continue;
    out = out ? `${out}${/[.!?…]$/.test(out) ? ' ' : '. '}${part}` : part;
  }
  return out;
}

/**
 * Запрос, который кнопка «Спросить нейросеть» кладёт в буфер. Ссылка — на
 * текст главы (`.md`, internal/seo/render_llm.go): длинная глава приходит
 * частями, и запрос просит модель дочитать их все. Адрес — канонический и
 * абсолютный: читатель вставит его в чужой чат, где относительный не значит
 * ничего.
 */
export function askAiPrompt(work: Work, chapter: Chapter, origin: string): string {
  const from = printedFolio(chapter.start_page, work);
  const to = printedFolio(chapter.end_page, work);
  const number = shownVolumeNumber(work);
  const part = shownVolumePart(work);
  const volume = number ? `т. ${number}${part ? ` ${part}` : ''}` : '';
  // work.title у корпуса повторяет название собрания («… Сочинения. Том 1»),
  // поэтому у тома источник собирается из собрания и номера, а не из заглавия.
  // У собрания с условной нумерацией номер не печатается, и том называет его
  // заглавие («XII пленум ИККИ») вслед за собранием.
  const parts = number
    ? [work.author, work.edition_title, volume]
    : work.volume_order_only
      ? [work.author, work.edition_title, work.title]
      : [work.author, work.title];
  const source = joinSourceParts(parts);
  const pages = from && to ? `с. ${from}—${to}` : '';
  const about = [source, pages].filter(Boolean).join(', ');
  const url = `${origin}${chapterPath(work, chapter)}.md`;
  return [
    `Прочитай главу «${chapter.title}»${about ? ` (${about})` : ''}:`,
    url,
    'Если текст разбит на части, дочитай все по ссылкам «Продолжение».',
    'Отвечай только по этому тексту и указывай страницы в квадратных скобках.',
    '',
    'Мой вопрос: ',
  ].join('\n');
}

/**
 * Запрос для понятия предметного указателя: ссылка на его текст
 * (`/concepts/{слаг}.md`, internal/seo/render_concept_llm.go) — статья,
 * подрубрики и все места в томах. Большое понятие приходит многими частями.
 *
 * rubricPath — подрубрика, выбранная на странице понятия: текст сужается до
 * неё тем же `?rubric_path=`, что стоит в адресе страницы. Сервер разбирает
 * значение и пересобирает в свою каноническую форму (internal/seo,
 * rubricQuery), так что написание параметра ни на что не влияет.
 */
export function askAiConceptPrompt(
  concept: Pick<Concept, 'slug' | 'title'>,
  origin: string,
  rubricPath: readonly string[] = [],
): string {
  let url = `${origin}/concepts/${concept.slug}.md`;
  let what = `понятие «${concept.title}»`;
  if (rubricPath.length > 0) {
    url += `?${new URLSearchParams({ rubric_path: encodeRubricPath(rubricPath) })}`;
    what = `подрубрику «${rubricPath.join(' / ')}» понятия «${concept.title}»`;
  }
  return [
    `Прочитай ${what} из предметного указателя читальни:`,
    url,
    'Если текст разбит на части, дочитай все по ссылкам «Продолжение».',
    'Отвечай только по этому тексту и указывай том и страницу печатного издания.',
    '',
    'Мой вопрос: ',
  ].join('\n');
}
