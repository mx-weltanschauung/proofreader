import { describe, it, expect } from 'vitest';
import { askAiPrompt, askAiConceptPrompt } from './askAi';
import type { Chapter, Work } from '../types';

const work = {
  id: 49,
  slug: 'lenin-t06',
  title: 'Полное собрание сочинений. Том 6',
  author: '',
  edition_title: 'В. И. Ленин. Полное собрание сочинений',
  volume_number: 6,
  page_offset: 0,
  numbering_style: 'arabic',
} as unknown as Work;

const chapter = {
  id: 10125,
  slug: 'chto-delat',
  title: 'Что делать?',
  start_page: 1,
  end_page: 192,
} as unknown as Chapter;

describe('askAiPrompt', () => {
  it('ведёт на канонический абсолютный адрес текста главы', () => {
    const text = askAiPrompt(work, chapter, 'https://lib.example.org');
    expect(text).toContain(
      'https://lib.example.org/works/49-lenin-t06/chapters/10125-chto-delat.md\n',
    );
  });

  it('называет главу, источник и страницы, пропуская пустое', () => {
    const text = askAiPrompt(work, chapter, 'https://lib.example.org');
    expect(text.split('\n')[0]).toBe(
      'Прочитай главу «Что делать?» (В. И. Ленин. Полное собрание сочинений. т. 6, с. 1—192):',
    );
  });

  it('просит дочитать части и ссылаться на страницы, кончается приглашением к вопросу', () => {
    const text = askAiPrompt(work, chapter, 'https://lib.example.org');
    expect(text).toContain('по ссылкам «Продолжение»');
    expect(text).toContain('указывай страницы в квадратных скобках');
    expect(text.endsWith('Мой вопрос: ')).toBe(true);
  });

  it('печатает страницы римскими у передних листов', () => {
    const front = { ...work, numbering_style: 'roman' } as Work;
    expect(askAiPrompt(front, { ...chapter, start_page: 1, end_page: 4 }, 'https://x')).toContain(
      'с. I—IV',
    );
  });

  const first = (w: Partial<Work>) =>
    askAiPrompt({ ...work, ...w } as Work, chapter, 'https://x').split('\n')[0];

  it('не дублирует собрание и не удваивает точку у реального тома', () => {
    const line = first({
      author: '',
      edition_title: 'К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.',
      title: 'К. Маркс и Ф. Энгельс. Сочинения. Том 1',
      volume_number: 1,
    });
    expect(line).toContain('(К. Маркс и Ф. Энгельс. Сочинения, 2-е изд. т. 1, с. 1—192)');
    expect(line).not.toContain('..');
  });

  it('добавляет полутом к номеру', () => {
    expect(first({ volume_number: 25, volume_part: 'II' })).toContain('т. 25 II');
  });

  it('без номера тома берёт заглавие работы', () => {
    expect(
      first({ volume_number: undefined, edition_title: undefined, title: 'Указатель' }),
    ).toContain('(Указатель, с. 1—192)');
  });

  it('без знака конца у собрания ставит точку с пробелом', () => {
    expect(first({ edition_title: 'Издание', volume_number: 3 })).toContain('Издание. т. 3');
  });
});

describe('askAiConceptPrompt', () => {
  it('ведёт на абсолютный адрес текста понятия и просит дочитать части', () => {
    const text = askAiConceptPrompt(
      { slug: 'abstraktnyj-trud', title: 'Абстрактный труд' },
      'https://lib.example.org',
    );
    expect(text.split('\n')[0]).toBe(
      'Прочитай понятие «Абстрактный труд» из предметного указателя читальни:',
    );
    expect(text).toContain('https://lib.example.org/concepts/abstraktnyj-trud.md\n');
    expect(text).toContain('дочитай все по ссылкам «Продолжение»');
    expect(text).toContain('указывай том и страницу');
    expect(text.endsWith('Мой вопрос: ')).toBe(true);
  });

  // Вид параметра — тот же, что страница понятия кладёт в свой адрес. Для
  // кириллицы, пробела и тире он совпадает и с каноном сервера (internal/seo,
  // rubricQuery; тот же пример — в render_concept_llm_test.go).
  it('с выбранной подрубрикой ведёт на её текст и называет её', () => {
    const text = askAiConceptPrompt({ slug: 'kpss', title: 'КПСС' }, 'https://lib.example.org', [
      'КПСС — съезды',
      'II съезд',
    ]);
    expect(text.split('\n')[0]).toBe(
      'Прочитай подрубрику «КПСС — съезды / II съезд» понятия «КПСС» из предметного указателя читальни:',
    );
    expect(text.split('\n')[1]).toBe(
      'https://lib.example.org/concepts/kpss.md?rubric_path=' +
        '%25D0%259A%25D0%259F%25D0%25A1%25D0%25A1%2520%25E2%2580%2594%2520%25D1%2581%25D1%258A%25D0%25B5%25D0%25B7%25D0%25B4%25D1%258B' +
        '%3AII%2520%25D1%2581%25D1%258A%25D0%25B5%25D0%25B7%25D0%25B4',
    );
  });

  it('пустой путь — понятие целиком', () => {
    const text = askAiConceptPrompt({ slug: 'x', title: 'X' }, 'https://h', []);
    expect(text.split('\n')[1]).toBe('https://h/concepts/x.md');
  });
});
