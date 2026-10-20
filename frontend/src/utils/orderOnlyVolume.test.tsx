import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { Chapter, EditionHighlight, VolumeSummary, Work } from '../types';
import { shownVolumeNumber, shownVolumePart } from './shownVolume';
import { citationSignature, signatureVolume } from './citation';
import { askAiPrompt } from './askAi';
import { volumeCoordinates } from './volumeCoordinates';
import { highlightCoordinate } from './highlightLabel';
import { spineNumber, spineNumberParts, volumeName } from './volumeLabel';
import { shelfSlots } from './shelfGeometry';
import { VolumeSlab } from '../components/VolumeSlab';
import { VolumeCard } from '../components/VolumeCard';

/**
 * Сторож условной нумерации томов (`Edition.volume_numbering === 'order'`,
 * миграция 000039). Номер тома такого собрания выдуман ради порядка на полке:
 * ни одна подпись для читателя не печатает его как «т. N» / «Том N». Каждая
 * поверхность проверяется парой: с признаком номера нет, без признака — он
 * на месте как раньше. Иначе тест проходил бы и на подписи, которая гасит
 * номер всем подряд.
 */

const NUMBER = 24;

function work(orderOnly: boolean): Work {
  return {
    id: 654,
    slug: 'xii-plenum',
    title: 'XII пленум ИККИ',
    author: '',
    edition_title: 'Коммунистический Интернационал. Стенографические отчёты',
    volume_number: NUMBER,
    volume_part: 'III',
    volume_order_only: orderOnly,
    page_offset: 0,
    numbering_style: 'arabic',
  } as unknown as Work;
}

function summary(orderOnly: boolean, over: Partial<VolumeSummary> = {}): VolumeSummary {
  return {
    ...work(orderOnly),
    volume_part: undefined,
    pages_total: 300,
    pages_by_status: {},
    chapters_total: 0,
    top_chapters: [],
    shelf_label: '',
    ...over,
  } as unknown as VolumeSummary;
}

const both = [
  { orderOnly: true, shows: false },
  { orderOnly: false, shows: true },
] as const;

describe('условная нумерация томов: номер не печатается читателю', () => {
  it.each(both)('shownVolumeNumber/shownVolumePart (order=$orderOnly)', ({ orderOnly, shows }) => {
    expect(shownVolumeNumber(work(orderOnly))).toBe(shows ? NUMBER : undefined);
    expect(shownVolumePart(work(orderOnly))).toBe(shows ? 'III' : undefined);
  });

  it.each(both)('подпись цитаты (order=$orderOnly)', ({ orderOnly, shows }) => {
    const w = work(orderOnly);
    const text = citationSignature({
      author: '',
      workTitle: '',
      editionTitle: w.edition_title ?? '',
      volumeTitle: w.title,
      ...signatureVolume(w),
      folios: ['37'],
      pageNumbers: [37],
    });
    expect(text).toBe(
      shows
        ? 'Коммунистический Интернационал. Стенографические отчёты, т. 24, ч. III, с. 37.'
        : 'Коммунистический Интернационал. Стенографические отчёты, с. 37.',
    );
  });

  it.each(both)('запрос «Спросить нейросеть» (order=$orderOnly)', ({ orderOnly, shows }) => {
    const chapter = { id: 1, slug: 'doklad', title: 'Доклад', start_page: 1, end_page: 9 };
    const first = askAiPrompt(
      work(orderOnly),
      chapter as unknown as Chapter,
      'https://x.org',
    ).split('\n')[0];
    expect(/т\. 24/.test(first)).toBe(shows);
    if (!shows) expect(first).toContain('XII пленум ИККИ');
  });

  it.each(both)('координаты тома в шапке и таблице (order=$orderOnly)', ({ orderOnly, shows }) => {
    expect(volumeCoordinates(work(orderOnly))).toBe(shows ? 'т. 24, III' : '');
  });

  it.each(both)('координата избранного (order=$orderOnly)', ({ orderOnly, shows }) => {
    const h: Pick<EditionHighlight, 'volume_number' | 'volume_part' | 'volume_order_only'> = {
      volume_number: NUMBER,
      volume_part: null,
      volume_order_only: orderOnly,
    };
    expect(highlightCoordinate(h)).toBe(shows ? 'т. 24' : '');
  });

  it.each(both)('клеймо и имя тома на полке (order=$orderOnly)', ({ orderOnly, shows }) => {
    const v = summary(orderOnly);
    expect(spineNumberParts(v).number).toBe(shows ? '24' : '');
    expect(spineNumber(v)).toBe(shows ? '24' : '');
    expect(volumeName(v)).toBe(shows ? 'Том 24' : 'XII пленум ИККИ');
  });

  it.each(both)('полка без выдуманных пропусков (order=$orderOnly)', ({ orderOnly, shows }) => {
    const slots = shelfSlots([summary(orderOnly)], 30);
    const gaps = slots.filter((s) => s.kind === 'gap');
    // Печатная нумерация: пустые места 1—23 и хвост до плана. Условная — одна
    // книга, без «Тома 1—23 ещё не сняты».
    expect(gaps.length > 0).toBe(shows);
    expect(slots.filter((s) => s.kind === 'volume')).toHaveLength(1);
  });

  it('условная полка держит порядок по номеру', () => {
    const late = summary(true, { id: 2, title: 'XII пленум', volume_number: 24 });
    const early = summary(true, { id: 1, title: 'IV конгресс', volume_number: 7 });
    const slots = shelfSlots([late, early]);
    expect(slots.map((s) => (s.kind === 'volume' ? s.volume.id : 0))).toEqual([1, 2]);
  });

  it.each(both)('корешок и карточка тома (order=$orderOnly)', ({ orderOnly, shows }) => {
    const v = summary(orderOnly);
    const { unmount } = render(
      <MemoryRouter>
        <VolumeSlab volume={v} current={false} />
        <VolumeCard
          volume={v}
          id="card"
          anchor={{ left: 0, top: 0, right: 0, bottom: 0 } as DOMRect}
        />
      </MemoryRouter>,
    );
    const link = screen.getByRole('link');
    // Имя книги для чтения с экрана: без номера — заглавие, а не оборванное «Том ».
    expect(link.getAttribute('aria-label')).toBe(shows ? 'Том 24' : 'XII пленум ИККИ');
    expect(/24/.test(link.textContent ?? '')).toBe(shows);
    expect(/Том 24/.test(screen.getByRole('tooltip').textContent ?? '')).toBe(shows);
    unmount();
  });
});
