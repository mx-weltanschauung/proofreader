import { describe, it, expect } from 'vitest';
import type { Chapter, Work } from '../types';
import { citationSignature } from './citation';
import { articleAt, citationPlace, citationPlaceFromLevels, issueOf } from './citationPlace';
import { chapterLevelsForPage } from '../hooks/useChaptersForPage';

function ch(
  over: Partial<Chapter> & Pick<Chapter, 'id' | 'title' | 'start_page' | 'end_page'>,
): Chapter {
  return {
    work_id: 1,
    type: 'chapter',
    order_number: over.id,
    is_apparatus: false,
    created_at: '',
    updated_at: '',
    ...over,
  };
}

const ISSUE = {
  id: 1,
  title: 'Под знаменем марксизма, 1928, № 12',
  author: '',
  page_offset: 0,
  journal_issue: {
    issue_id: 1,
    journal_id: 1,
    journal_slug: 'pzm',
    journal_title: 'Под знаменем марксизма',
    year: 1928,
    label: '12',
    months: '',
  },
} as unknown as Work;

const VOLUME = {
  id: 2,
  title: 'Т. 6',
  author: 'В. И. Ленин',
  edition_title: 'Сочинения',
  page_offset: 0,
} as unknown as Work;

// Номер: статья верхнего уровня (1—4), рубрика «Критика и библиография»
// (5—9) с двумя рецензиями внутри (6—7, 7—9); полоса 5 — заголовок рубрики,
// статьи на ней нет.
const firstArticle = ch({
  id: 10,
  title: 'О диалектике',
  start_page: 1,
  end_page: 4,
  article_kind: 'статья',
  credits: [{ position: 1, role: 'author', printed: 'И. Рубин' }],
});
const review1 = ch({
  id: 21,
  title: 'Рецензия первая',
  start_page: 6,
  end_page: 7,
  article_kind: 'рецензия',
  credits: [{ position: 1, role: 'author', printed: 'А. Деборин' }],
});
const review2 = ch({
  id: 22,
  title: 'Рецензия вторая',
  start_page: 7,
  end_page: 9,
  article_kind: 'рецензия',
  credits: [{ position: 1, role: 'author', printed: 'Л. Аксельрод' }],
});
const rubric = ch({
  id: 20,
  title: 'Критика и библиография',
  start_page: 5,
  end_page: 9,
  children: [review1, review2],
});
const TREE = [firstArticle, rubric];

describe('citationPlace — номер журнала', () => {
  it('статья верхнего уровня — заглавие и автор из подписи статьи', () => {
    expect(citationPlace(ISSUE, TREE, 2)).toEqual({
      workTitle: 'О диалектике',
      author: 'И. Рубин',
    });
  });

  it('статья внутри рубрики — сама статья, а не имя рубрики', () => {
    expect(citationPlace(ISSUE, TREE, 6)).toEqual({
      workTitle: 'Рецензия первая',
      author: 'А. Деборин',
    });
  });

  it('полоса рубрики без статьи — пустые слоты, подпись номера', () => {
    const place = citationPlace(ISSUE, TREE, 5);
    expect(place).toEqual({ workTitle: '', author: '' });
    expect(
      citationSignature({
        ...place,
        editionTitle: '',
        volumeTitle: ISSUE.title,
        folios: ['5'],
        pageNumbers: [5],
        issue: issueOf(ISSUE),
      }),
    ).toBe('Под знаменем марксизма. 1928. № 12. С. 5.');
  });

  it('стык двух статей: без читаемой главы — первая, внутри читаемой — она', () => {
    expect(citationPlace(ISSUE, TREE, 7).workTitle).toBe('Рецензия первая');
    expect(citationPlace(ISSUE, TREE, 7, review2).workTitle).toBe('Рецензия вторая');
  });

  it('читаемая глава — рубрика: статья внутри неё', () => {
    expect(citationPlace(ISSUE, TREE, 8, rubric)).toEqual({
      workTitle: 'Рецензия вторая',
      author: 'Л. Аксельрод',
    });
  });

  it('раздел внутри статьи без вида — подпись объемлющей статьи', () => {
    const section = ch({ id: 11, title: 'I', start_page: 2, end_page: 3 });
    const tree = [{ ...firstArticle, children: [section] }, rubric];
    expect(citationPlace(ISSUE, tree, 2, section)).toEqual({
      workTitle: 'О диалектике',
      author: 'И. Рубин',
    });
  });

  it('статья внутри статьи (документ в публикации) — самая глубокая', () => {
    const doc = ch({
      id: 12,
      title: 'Письмо в редакцию',
      start_page: 3,
      end_page: 4,
      article_kind: 'документ',
      credits: [{ position: 1, role: 'author', printed: 'Г. Плеханов' }],
    });
    const tree = [{ ...firstArticle, children: [doc] }, rubric];
    expect(citationPlace(ISSUE, tree, 3)).toEqual({
      workTitle: 'Письмо в редакцию',
      author: 'Г. Плеханов',
    });
    expect(citationPlace(ISSUE, tree, 2).workTitle).toBe('О диалектике');
  });

  it('по уровням (полоса со сканом) — то же правило', () => {
    expect(citationPlaceFromLevels(ISSUE, chapterLevelsForPage(TREE, 8)).workTitle).toBe(
      'Рецензия вторая',
    );
    expect(articleAt([])).toBeNull();
  });
});

describe('citationPlace — том', () => {
  it('неаппаратная глава верхнего уровня и автор тома', () => {
    const tree = [
      ch({
        id: 1,
        title: 'Указатель',
        start_page: 1,
        end_page: 3,
        is_apparatus: true,
        order_number: 0,
      }),
      ch({
        id: 2,
        title: 'Что делать?',
        start_page: 1,
        end_page: 3,
        children: [ch({ id: 3, title: 'I', start_page: 1, end_page: 2 })],
      }),
    ];
    expect(citationPlace(VOLUME, tree, 1)).toEqual({
      workTitle: 'Что делать?',
      author: 'В. И. Ленин',
    });
    expect(issueOf(VOLUME)).toBeUndefined();
  });

  it('у номера журнальные координаты', () => {
    expect(issueOf(ISSUE)).toEqual({ journal: 'Под знаменем марксизма', year: 1928, label: '12' });
  });
});
