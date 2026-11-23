import { chaptersApi, pagesApi, worksApi } from '../services/api';
import type { Chapter, MarginNote, Work } from '../types';
import { findChapterById } from './chapterTree';
import {
  citationLinkMarkdown,
  citationMarkdown,
  citationSignature,
  signatureVolume,
} from './citation';
import { citationPlace, issueOf } from './citationPlace';
import { printedFolio } from './folio';
import { noteHref } from './marginNoteHref';
import { pageAnchorId } from './pageAnchor';
import { chapterPath, pagePath, workPath } from './paths';
import { flattenText, markQuote, normalizeQuote } from './quoteMatch';
import { chapterLevelsForPage } from '../hooks/useChaptersForPage';

// Поля заметки хранятся как простой текст. В Markdown сохраняем их смысл,
// включая угловые скобки, разметку и пустые строки внутри снимка цитаты.
function literal(text: string): string {
  return text
    .replace(/\r\n?/g, '\n')
    .replace(/&/g, '&amp;')
    .replace(/[<>"\\`*_[\]|~]/g, (ch) => '\\' + ch)
    .replace(
      /^(\s*)([-+#!~]|\d+[.)])/gm,
      (_, indent: string, marker: string) => indent + marker.slice(0, -1) + '\\' + marker.slice(-1),
    );
}

async function pageHtml(workId: number, number: number): Promise<string> {
  const { data: page } = await pagesApi.getByNumber(workId, number);
  const { data } = await pagesApi.render(workId, page.id);
  return data.html;
}

function fragment(html: string): HTMLElement {
  const template = document.createElement('template');
  template.innerHTML = html;
  const root = document.createElement('div');
  root.append(template.content);
  return root;
}

type NoteLocation = { path: string; status?: 'missing' | 'start-only' | 'ambiguous' };

async function quoteLocation(
  note: MarginNote,
  work: Work,
  chapters: Chapter[],
  sourceChapter: Chapter | null,
  loadPage: (page: number) => Promise<string>,
): Promise<NoteLocation> {
  const start = note.start_page;
  const end = note.end_page;
  if (start === null) return { path: workPath(work), status: 'missing' };

  const plainPath = pagePath(work, start);
  const root = fragment(await loadPage(start));
  const params = new URLSearchParams();
  const key = note.quote_key;
  const tail = note.quote_tail;

  const match = key
    ? markQuote(root, { start: key, end: end === start ? tail : undefined })
    : 'miss';
  if (match === 'miss') {
    return { path: plainPath, status: 'missing' };
  }
  params.set('quote', key);

  if (end === null) return { path: `${plainPath}?${params}`, status: 'missing' };
  if (end === start) {
    if (tail && match !== 'partial') params.set('to', tail);
    const inChapter =
      sourceChapter && sourceChapter.start_page <= start && start <= sourceChapter.end_page;
    const path = inChapter
      ? `${chapterPath(work, sourceChapter)}?${params}#${pageAnchorId(start)}`
      : `${plainPath}?${params}`;
    const status = match === 'partial' ? 'missing' : match === 'multiple' ? 'ambiguous' : undefined;
    return { path, status };
  }

  const tailFound =
    !!tail && flattenText(fragment(await loadPage(end))).text.includes(normalizeQuote(tail));
  const coversRange = (c: Chapter) => c.start_page <= start && end <= c.end_page;
  const chapter =
    sourceChapter && coversRange(sourceChapter)
      ? sourceChapter
      : chapterLevelsForPage(chapters, start).flat().find(coversRange);
  if (tailFound && chapter) {
    params.set('to', tail);
    params.set('to_page', String(end));
    return {
      path: `${chapterPath(work, chapter)}?${params}#${pageAnchorId(start)}`,
      status: match === 'multiple' ? 'ambiguous' : undefined,
    };
  }
  return { path: `${plainPath}?${params}`, status: tailFound ? 'start-only' : 'missing' };
}

async function renderNote(
  note: MarginNote,
  work: Work,
  chapters: Chapter[],
  loadPage: (page: number) => Promise<string>,
  origin: string,
): Promise<string> {
  const start = note.start_page ?? note.snapshot_start_page;
  const end = note.end_page ?? note.snapshot_end_page ?? start;
  const pages = start === null ? [] : end === null || end === start ? [start] : [start, end];
  const chapter = note.chapter_id === null ? null : findChapterById(chapters, note.chapter_id);
  if (note.chapter_id !== null && !chapter) {
    throw new Error('Глава заметки стала недоступна. Обновите список и проверьте выбор.');
  }
  const place =
    note.target === 'quote' && start !== null
      ? citationPlace(work, chapters, start, chapter)
      : {
          author: work.journal_issue ? '' : (work.author ?? ''),
          workTitle: '',
        };
  if (note.target === 'chapter') {
    const article =
      chapter && work.journal_issue
        ? citationPlace(work, chapters, chapter.start_page, chapter)
        : place;
    place.author = article.author;
    place.workTitle = chapter?.title || note.chapter_title;
  }
  const signature = citationSignature({
    ...place,
    editionTitle: work.edition_title ?? '',
    volumeTitle: work.title,
    ...signatureVolume(work),
    folios: pages.map((n) => printedFolio(n, work)),
    pageNumbers: pages,
    issue: issueOf(work),
  });

  const link: NoteLocation =
    note.target === 'quote'
      ? await quoteLocation(note, work, chapters, chapter, loadPage)
      : note.target === 'chapter' && !chapter
        ? { path: workPath(work), status: 'missing' }
        : { path: noteHref(note).to, status: undefined };
  const url = `<${new URL(link.path, origin).href}>`;
  const quote = note.quote_text
    ? citationMarkdown(literal(note.quote_text), literal(signature), url)
    : citationLinkMarkdown(literal(signature), url);
  const status = {
    missing: note.target === 'chapter' ? 'Глава не найдена.' : 'Точное место цитаты не найдено.',
    'start-only': 'Ссылка ведёт к началу цитаты.',
    ambiguous: 'Начало цитаты встречается на полосе несколько раз.',
  };
  const notice = link.status ? `\n_${status[link.status]}_\n` : '';
  return `${quote}${notice}\n**Моя заметка**\n\n${literal(note.body)}`;
}

export async function notesMarkdown(notes: MarginNote[], origin: string): Promise<string> {
  const sources = new Map<number, Promise<{ work: Work; chapters: Chapter[] }>>();
  const pages = new Map<string, Promise<string>>();
  const texts: string[] = [];

  async function render(note: MarginNote): Promise<string> {
    const id = note.work_id;
    let source = sources.get(id);
    if (!source) {
      source = Promise.all([worksApi.get(id), chaptersApi.list(id)]).then(
        ([{ data: work }, { data: chapters }]) => ({ work, chapters: chapters ?? [] }),
      );
      sources.set(id, source);
    }
    const { work, chapters } = await source;
    return renderNote(
      note,
      work,
      chapters,
      (page) => {
        const key = `${id}/${page}`;
        let html = pages.get(key);
        if (!html) {
          html = pageHtml(id, page);
          pages.set(key, html);
        }
        return html;
      },
      origin,
    );
  }

  // Общие тома и полосы запрашиваем один раз, одновременно готовим до 8 записей.
  for (let i = 0; i < notes.length; i += 8) {
    texts.push(...(await Promise.all(notes.slice(i, i + 8).map(render))));
  }
  return `# Мои заметки\n\n${texts.join('\n\n---\n\n')}\n`;
}
