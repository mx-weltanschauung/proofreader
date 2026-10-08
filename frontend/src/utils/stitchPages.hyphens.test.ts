import { describe, expect, it } from 'vitest';
import { stitchPages } from './stitchPages';

/**
 * Все переносы через границу полосы в томе 62 (Ленин, ПСС, т. 8) — 39 штук,
 * выбранные из живой базы, а не придуманные.
 *
 * `inVolume` — слова тома, которые для этой пары что-то решают: сросшаяся
 * форма и те половины, что ходят по тексту самостоятельно. Обрывки самих
 * переносов в этот список не входят, иначе «предста» доказывало бы своё
 * существование само собой.
 *
 * Печатный дефис здесь ровно один — «оппортунистами-меньшевиками» на стр. 508.
 * Остальные 38 надо срастить.
 */
const HYPHENS: ReadonlyArray<{ page: number; left: string; right: string; inVolume: string }> = [
  { page: 27, left: 'предста', right: 'вительство', inVolume: 'представительство предста' },
  { page: 49, left: 'орга', right: 'низоваться', inVolume: 'организоваться орга' },
  { page: 73, left: 'марксист', right: 'ских', inVolume: 'марксистских марксист ских' },
  { page: 79, left: 'либера', right: 'лизму', inVolume: 'либерализму либера' },
  { page: 155, left: 'доступ', right: 'ных', inVolume: 'доступ' },
  { page: 159, left: 'необхо', right: 'димым', inVolume: 'необходимым' },
  { page: 195, left: 'свой', right: 'ство', inVolume: 'свойство свой' },
  { page: 203, left: 'представ', right: 'лении', inVolume: 'лении' },
  { page: 215, left: 'Органи', right: 'зационным', inVolume: 'организационным' },
  { page: 241, left: 'извест', right: 'ных', inVolume: 'известных' },
  { page: 245, left: 'пар', right: 'тию', inVolume: 'партию пар' },
  { page: 249, left: 'во', right: 'проса', inVolume: 'вопроса во' },
  { page: 261, left: 'Map', right: 'това', inVolume: '' },
  { page: 265, left: 'большин', right: 'ством', inVolume: 'большинством большин' },
  { page: 268, left: 'уста', right: 'новив', inVolume: '' },
  { page: 289, left: 'загранич', right: 'ном', inVolume: 'заграничном ном' },
  { page: 291, left: 'Ру', right: 'сова', inVolume: 'русова ру' },
  { page: 305, left: 'возбужде', right: 'ния', inVolume: 'возбуждения' },
  { page: 313, left: 'согла', right: 'шения', inVolume: 'соглашения' },
  { page: 315, left: 'заезжа', right: 'ние', inVolume: 'заезжание ние' },
  { page: 319, left: 'возмож', right: 'ности', inVolume: 'возможности ности' },
  { page: 349, left: 'преемствен', right: 'ность', inVolume: 'преемственность' },
  { page: 355, left: 'по', right: 'тому', inVolume: 'потому по тому' },
  { page: 359, left: 'фортифика', right: 'ционное', inVolume: '' },
  { page: 371, left: 'опроверг', right: 'нута', inVolume: 'опровергнута опроверг' },
  { page: 377, left: 'дис', right: 'циплины', inVolume: 'дисциплины' },
  { page: 379, left: 'интел', right: 'лигента', inVolume: 'интеллигента' },
  { page: 401, left: 'муд', right: 'ростью', inVolume: '' },
  { page: 409, left: 'пред', right: 'лагали', inVolume: 'предлагали пред' },
  {
    page: 508,
    left: 'оппортунистами',
    right: 'меньшевиками',
    inVolume: 'оппортунистами меньшевиками',
  },
  { page: 516, left: 'делега', right: 'та', inVolume: 'делегата делега та' },
  { page: 559, left: 'на', right: 'пример', inVolume: 'например на пример' },
  { page: 591, left: 'мар', right: 'ксистских', inVolume: 'марксистских' },
  { page: 601, left: 'демокра', right: 'тическом', inVolume: 'демократическом демокра' },
  { page: 603, left: 'Независи', right: 'мость', inVolume: 'независимость' },
  { page: 605, left: 'орга', right: 'ном', inVolume: 'органом орга ном' },
  { page: 607, left: 'последователь', right: 'ным', inVolume: 'последовательным ным' },
  { page: 619, left: 'Париж', right: 'ской', inVolume: 'парижской париж ской' },
  { page: 633, left: 'эконо', right: 'мизму', inVolume: 'экономизму' },
];

const PRINTED_HYPHEN = 508;

const seam = {
  href: (n: number) => `/works/62/pages/${n}`,
  label: (n: number) => `Страница ${n}`,
};

/** Слово, получившееся на шве, — без маркера номера и окружающего текста. */
function wordAtSeam(html: string): string {
  const host = document.createElement('div');
  host.innerHTML = html;
  host.querySelectorAll('.page-marker').forEach((m) => m.remove());
  const text = (host.textContent ?? '').replace(/\s+/g, ' ');
  return text.match(/попалось (.+?) и дальше\./)?.[1] ?? text;
}

describe('перенос слова через границу полосы', () => {
  it.each(HYPHENS)('стр. $page: $left|$right', ({ page, left, right, inVolume }) => {
    const out = stitchPages(
      [
        // Слова тома лежат в заголовке: заголовки в склейку не втягиваются, и
        // словарь остаётся единственным, на что эта страница влияет.
        { pageNumber: page - 2, html: `<h2>${inVolume}</h2>` },
        { pageNumber: page - 1, html: `<p>в тексте попалось ${left}-</p>` },
        { pageNumber: page, html: `<p>${right} и дальше.</p>` },
      ],
      seam,
    );

    const expected = page === PRINTED_HYPHEN ? `${left}-${right}` : `${left}${right}`;
    expect(wordAtSeam(out[1].html)).toBe(expected);
  });
});
