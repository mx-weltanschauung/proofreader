import { describe, it, expect } from 'vitest';
import type { VolumeSummary } from '../types';
import { corpusSentence } from './corpusSentence';

function volume(over: Partial<VolumeSummary> = {}): VolumeSummary {
  return {
    id: 1,
    title: 'том',
    author: '',
    language: '',
    country: '',
    file_path: '',
    status: 'draft',
    page_offset: 0,
    owner_id: 1,
    created_at: '',
    updated_at: '2026-01-01',
    volume_number: 1,
    pages_total: 0,
    pages_by_status: {},
    chapters_total: 0,
    ...over,
  } as VolumeSummary;
}

const volumePages = (pages: number) => ({ pages_total: pages }) as VolumeSummary;

/**
 * Пробелы к одному виду: разряды числа разделены неразрывным, и сверять его
 * здесь незачем — за это отвечает тест `groupThousands` в editionCaption.
 * Тут проверяется, что и сколько сказано.
 */
const plain = (s: string) => s.replace(/\s/g, ' ');

describe('corpusSentence', () => {
  it('складывает тома и страницы по всем собраниям, а не по первому', () => {
    const shelves = [
      { volumes: [volumePages(663), volumePages(678)] },
      { volumes: [volumePages(331)] },
    ];
    expect(plain(corpusSentence(shelves))).toMatch(/^3 тома, 1 672 страницы\./);
  });

  it('склоняет число томов и число страниц каждое по себе', () => {
    // Два тома (мн. «тома») при 1202 страницах (мн. «страницы») — формы
    // независимы, и одна на двоих дала бы «2 тома, 1 202 страниц».
    expect(plain(corpusSentence([{ volumes: [volumePages(601), volumePages(601)] }]))).toMatch(
      /^2 тома, 1 202 страницы\./,
    );
  });

  it('говорит, зачем сохранена печатная нумерация', () => {
    // Число само по себе не объясняет, чем эта читальня отличается от папки
    // сканов: ссылаться на страницу можно так же, как на бумажный том.
    expect(corpusSentence([{ volumes: [volumePages(600)] }])).toContain('печатная нумерация');
  });

  it('считает тома по номерам, как подписи собраний', () => {
    const sentence = corpusSentence([
      {
        volumes: [
          volume({ id: 1, volume_number: 26, volume_part: 'I', pages_total: 100 }),
          volume({ id: 2, volume_number: 26, volume_part: 'II', pages_total: 100 }),
          volume({ id: 3, volume_number: undefined, pages_total: 14 }),
        ],
      },
      { volumes: [volume({ id: 4, volume_number: 1, pages_total: 50 })] },
    ]);
    expect(sentence).toMatch(/^2 тома, 264 страницы\./);
  });

  it('у пустой читальни не выдумывает содержимого', () => {
    expect(corpusSentence([])).toBe('');
  });
});
