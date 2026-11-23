import { afterEach, expect, it, vi } from 'vitest';
import type { AxiosResponse } from 'axios';
import { chaptersApi, pagesApi, worksApi } from '../services/api';
import type { Chapter, MarginNote, Page, Work } from '../types';
import { notesMarkdown } from './notesMarkdown';

const origin = 'https://library.example';
const work = {
  id: 4,
  slug: 'zhurnal',
  title: 'Учебный журнал, № 3',
  page_offset: -1,
  journal_issue: { journal_title: 'Учебный журнал', year: 1924, label: '3' },
} as Work;
const article = {
  id: 6,
  slug: 'statya',
  title: 'Учебная статья',
  start_page: 1,
  end_page: 3,
  order_number: 1,
  article_kind: 'статья',
  credits: [{ position: 1, role: 'author', printed: 'А. Автор' }],
} as Chapter;
const note: MarginNote = {
  id: 1,
  work_id: 4,
  work_slug: work.slug,
  target: 'quote',
  chapter_id: null,
  chapter_title: '',
  start_page: 1,
  end_page: 1,
  page_missing: false,
  snapshot_start_page: 1,
  snapshot_end_page: 1,
  quote_key: 'Слова автора',
  quote_tail: 'в примере.',
  quote_text: 'Слова автора в примере.',
  body: 'Слова читателя.',
  created_at: '',
  updated_at: '',
};

function response<T>(data: T): AxiosResponse<T> {
  return { data } as AxiosResponse<T>;
}

function mockSource(html: string) {
  vi.spyOn(worksApi, 'get').mockResolvedValue(response(work));
  vi.spyOn(chaptersApi, 'list').mockResolvedValue(response([article]));
  vi.spyOn(pagesApi, 'getByNumber').mockResolvedValue(response({ id: 7 } as Page));
  vi.spyOn(pagesApi, 'render').mockResolvedValue(response({ html }));
}

afterEach(() => vi.restoreAllMocks());

it('подписывает журнальную цитату без колонцифры и даёт абсолютный адрес главы', async () => {
  mockSource('<p>Слова автора в примере.</p>');
  const markdown = await notesMarkdown(
    [
      note,
      {
        ...note,
        target: 'chapter',
        chapter_id: article.id,
        chapter_title: article.title,
        start_page: null,
        end_page: null,
        snapshot_start_page: null,
        snapshot_end_page: null,
        quote_text: '',
      },
    ],
    origin,
  );
  expect(markdown).toContain('> Слова автора в примере.');
  expect(markdown).toContain(
    'А. Автор. Учебная статья // Учебный журнал. 1924. № 3. Б/н, полоса 1.',
  );
  expect(markdown).toContain('<https://library.example/works/4-zhurnal/pages/1?quote=');
  expect(markdown).toContain('<https://library.example/works/4-zhurnal/chapters/6>');
  expect(markdown).toContain('**Моя заметка**\n\nСлова читателя.');
});

it('при пропаже якоря сохраняет снимок и ведёт на полосу без сломанного якоря', async () => {
  mockSource('<p>Изменённый текст.</p>');
  const markdown = await notesMarkdown([note], origin);
  expect(markdown).toContain('> Слова автора в примере.');
  expect(markdown).toContain('<https://library.example/works/4-zhurnal/pages/1>');
  expect(markdown).toContain('_Точное место цитаты не найдено._');
});

it('использует сохранённую статью, если на полосе начинаются две статьи', async () => {
  mockSource('<p>Слова автора в примере.</p>');
  const secondArticle = {
    ...article,
    id: 8,
    slug: 'vtoraya-statya',
    title: 'Вторая статья',
    order_number: 2,
    credits: [{ position: 1, role: 'author', printed: 'Б. Автор' }],
  } satisfies Chapter;
  vi.mocked(chaptersApi.list).mockResolvedValue(response([article, secondArticle]));

  const markdown = await notesMarkdown([{ ...note, chapter_id: 8 }], origin);
  expect(markdown).toContain('Б. Автор. Вторая статья // Учебный журнал.');
  expect(markdown).toContain('/chapters/8-vtoraya-statya?quote=');
});

it('прерывает сборку, если существующая глава уже отсутствует в доступном дереве', async () => {
  mockSource('<p>Слова автора в примере.</p>');
  await expect(notesMarkdown([{ ...note, chapter_id: 42 }], origin)).rejects.toThrow(
    'Глава заметки стала недоступна',
  );
});
