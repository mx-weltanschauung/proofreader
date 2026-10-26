import type {
  Edition,
  EventOrganizationSummary,
  JournalSummary,
  ShelfWork,
  VolumeSummary,
} from '../types';

/**
 * Раскладка главной по разделам. Из неё одной рисуются и оглавление вверху
 * страницы, и полки под ним: собрание есть в оглавлении ровно тогда, когда
 * есть его полка, и число томов у них общее.
 *
 * Разделы — данные (`shelf_group` у собрания и у организации событий), а не
 * код: в коде названо только умолчание.
 */
export const DEFAULT_SECTION = 'Собрания сочинений';

export interface HomeShelf {
  edition: Edition;
  /** Тома полки, уже без предисловий (`splitEditionVolumes(...).shelf`). */
  volumes: VolumeSummary[];
  anchor: string;
  /** Заглавие без префикса «{раздел}. » — для оглавления и заголовка полки. */
  shortTitle: string;
}

export interface HomeSection {
  name: string;
  anchor: string;
  shelves: HomeShelf[];
  organizations: EventOrganizationSummary[];
}

export interface HomeLayout {
  sections: HomeSection[];
  journals: JournalSummary[];
  /** Организации без раздела — блок «Съезды и конгрессы» после разделов. */
  looseOrganizations: EventOrganizationSummary[];
  looseWorks: ShelfWork[];
}

/**
 * Порядок полок: сперва те, которых в читальне больше. Прежде собрания шли по
 * алфавиту заголовка — то есть по фамилии автора, — и порядок читался
 * случайным: сорок пять ленинских томов стояли выше четырёх плехановских не
 * почему-либо, а потому что «В» раньше «Г».
 */
export function byHoldings(a: { volumes: VolumeSummary[] }, b: { volumes: VolumeSummary[] }): number {
  if (a.volumes.length !== b.volumes.length) return b.volumes.length - a.volumes.length;
  const pages = (s: { volumes: VolumeSummary[] }) => s.volumes.reduce((sum, v) => sum + v.pages_total, 0);
  return pages(b) - pages(a);
}

/** Якорь полки — по адресному слагу собрания: ссылку «/#…» можно переслать. */
export function editionAnchor(edition: Pick<Edition, 'slug' | 'url_slug'>): string {
  return `ed-${edition.url_slug || edition.slug}`;
}

export function organizationAnchor(org: { slug: string }): string {
  return `org-${org.slug}`;
}

/**
 * Заглавие собрания под своим разделом: «Раздел. Отчёты» под «Разделом» —
 * просто «Отчёты». Срезается только префикс целиком, с точкой и пробелом, и
 * только если после него что-то остаётся; раздел по умолчанию не срезает
 * ничего.
 */
export function shortEditionTitle(title: string, section: string): string {
  if (section === DEFAULT_SECTION) return title;
  const prefix = `${section}. `;
  if (!title.startsWith(prefix)) return title;
  const rest = title.slice(prefix.length).trim();
  return rest || title;
}

/**
 * Делит заглавие на автора и остаток по первой «. », перед которой стоит
 * слово длиннее одной буквы: инициалы («Г. В. Ф.») делением не считаются.
 * Не делится — всё заглавие идёт автором.
 */
export function splitAuthor(title: string): { author: string; rest: string } {
  const re = /(\p{L}+)\. /gu;
  for (let m = re.exec(title); m !== null; m = re.exec(title)) {
    if (m[1].length > 1) {
      const end = m.index + m[1].length;
      const rest = title.slice(end + 2).trim();
      if (rest) return { author: title.slice(0, end), rest };
    }
  }
  return { author: title, rest: '' };
}

const groupOf = (value: string | undefined): string => (value ?? '').trim() || DEFAULT_SECTION;
const volumesIn = (section: HomeSection) => section.shelves.reduce((sum, s) => sum + s.volumes.length, 0);

export function buildHomeLayout(
  shelves: { edition: Edition; volumes: VolumeSummary[] }[],
  organizations: EventOrganizationSummary[],
  journals: JournalSummary[],
  looseWorks: ShelfWork[],
): HomeLayout {
  const byName = new Map<string, HomeSection>();
  const sectionFor = (name: string): HomeSection => {
    let section = byName.get(name);
    if (!section) {
      section = { name, anchor: '', shelves: [], organizations: [] };
      byName.set(name, section);
    }
    return section;
  };

  for (const { edition, volumes } of shelves) {
    const name = groupOf(edition.shelf_group);
    sectionFor(name).shelves.push({
      edition,
      volumes,
      anchor: editionAnchor(edition),
      shortTitle: shortEditionTitle(edition.title, name),
    });
  }
  const looseOrganizations: EventOrganizationSummary[] = [];
  for (const org of organizations) {
    if ((org.shelf_group ?? '').trim()) sectionFor(groupOf(org.shelf_group)).organizations.push(org);
    else looseOrganizations.push(org);
  }

  const sections = [...byName.values()];
  for (const section of sections) section.shelves.sort(byHoldings);
  sections.sort((a, b) => {
    if (a.name === DEFAULT_SECTION) return -1;
    if (b.name === DEFAULT_SECTION) return 1;
    return volumesIn(b) - volumesIn(a) || a.name.localeCompare(b.name, 'ru');
  });
  sections.forEach((section, i) => (section.anchor = `razdel-${i + 1}`));

  return { sections, journals, looseOrganizations, looseWorks };
}
