import type { Edition } from '../types';
import { toRoman, type FolioSource } from './folio';
import { parsePrintedPage } from './pageJump';

interface PrintedReference {
  source: string;
  volume: number;
  part?: string;
  page: string;
}

// Маркеры тома и страницы отделяют их номера от года и номера издания.
const REFERENCE =
  /^(?<source>.+?)[\s,;—–]+(?:т\.|том(?=\s))\s*(?<volume>\d+)(?:\s*[·/]\s*(?<spinePart>[ivxlcdm]+|\d+)|[\s,.;—–]+(?:ч\.|часть)\s*(?<part>[ivxlcdm]+|\d+))?[\s,.;—–]+(?:с\.|стр\.|страница)\s*(?<page>\d+|[ivxlcdm]+)(?:\s*[-–—]\s*(?:\d+|[ivxlcdm]+))?[\s.»,;)]*$/iu;

export function parsePrintedReference(input: string): PrintedReference | null {
  const sourceText = input
    .normalize('NFKC')
    .replace(/^>.*$/gm, '')
    .replace(/https?:\/\/\S+\s*$/iu, '')
    .replace(/\s+/g, ' ')
    .trim()
    .split('//')
    .pop()
    ?.trim();
  const match = sourceText?.match(REFERENCE);
  if (!match?.groups) return null;

  const { source, volume, part, spinePart, page } = match.groups;
  const number = Number(volume);
  if (!Number.isSafeInteger(number) || number < 1 || !source.trim()) return null;
  if (/^\d+$/.test(page) && !Number.isSafeInteger(Number(page))) return null;
  if (parsePrintedPage(page, { page_offset: 0 }) === null) return null;

  return {
    source: source.trim(),
    volume: number,
    part: part ?? spinePart,
    page: page.toUpperCase(),
  };
}

const EDITION_NUMBER =
  /(^|[\s,(;])(?:(\d+)(?:\s*[-‐‑–—]\s*(?:е|ое))?\s*изд(?:ание)?\.?|изд(?:ание)?\.?\s*(\d+))(?=$|[\s,.;)])/iu;

function editionDesignation(text: string) {
  const match = text.match(EDITION_NUMBER);
  return {
    title: text.replace(EDITION_NUMBER, '$1').trim(),
    number: match ? Number(match[2] ?? match[3]) : undefined,
  };
}

const ABBREVIATED_WORDS = new Map([
  ['полн', 'полное'],
  ['собр', 'собрание'],
  ['соч', 'сочинения'],
  ['сочинений', 'сочинения'],
]);

function titleWords(text: string): string[] {
  return (
    text
      .replace(/(?<!\p{L})(?:\p{L}\.){2,}\p{L}(?!\p{L})/gu, (word) => word.replace(/\./g, ''))
      .toLowerCase()
      .replace(/ё/g, 'е')
      .match(/[\p{L}\d]+/gu) ?? []
  )
    .filter((word) => word.length > 1 || /^\d$/.test(word))
    .map((word) => ABBREVIATED_WORDS.get(word) ?? word);
}

function containsWords(title: string[], query: string[]): boolean {
  let matched = 0;
  for (const word of title) {
    const token = query[matched];
    if (word === token || (word.length >= 4 && (word + 'а' === token || word + 'я' === token))) {
      matched += 1;
      if (matched === query.length) return true;
    }
  }
  return false;
}

export function matchingEditions(source: string, editions: Edition[]): Edition[] {
  const requested = editionDesignation(source.replace(/^[\s(«]*см\.\s*:?\s*/iu, ''));
  const query = titleWords(requested.title);
  return editions.filter((edition) => {
    if (edition.volume_numbering === 'order') return false;
    const actual = editionDesignation(edition.title.normalize('NFKC'));
    if (requested.number !== undefined && requested.number !== actual.number) return false;
    if (query.length === 0) return requested.number !== undefined;
    if (containsWords(titleWords(actual.title), query)) return true;

    // Сокращение книжного заглавия бывает и после фамилии, и перед ней.
    const segments = actual.title.split(/(?<=\p{L}{2})\.\s+/u);
    const bookTitle = segments.pop() ?? '';
    const bookWords = titleWords(bookTitle.replace(/\s+в\s+\d+\s+томах.*$/iu, ''));
    if (bookWords.length < 2) return false;
    const abbreviation = bookWords.map((word) => word[0]).join('');
    const authors = titleWords(segments.join(' '));
    return (
      containsWords([...authors, abbreviation], query) ||
      containsWords([abbreviation, ...authors], query)
    );
  });
}

export function sameVolumePart(requested: string, part: string | undefined): boolean {
  if (part === undefined) return false;
  const roman = (value: string) => {
    const number = Number(value);
    return /^\d+$/.test(value) && number >= 1 && number <= 3999
      ? toRoman(number)
      : value.toUpperCase();
  };
  return roman(requested) === roman(part.trim());
}

export function printedReferencePage(page: string, work: FolioSource): number | null {
  const isRoman = !/^\d+$/.test(page);
  if (isRoman !== (work.numbering_style === 'roman')) return null;
  return parsePrintedPage(page, work);
}
