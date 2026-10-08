import { Fragment } from 'react';
import ReactMarkdown from 'react-markdown';
import { Link } from 'react-router-dom';

import type { ConceptArticle, ConceptReference } from '../types';
import { entryAnchorId } from '../utils/conceptEntries';
import {
  groupByRubric,
  hasPathPrefix,
  pathOf,
  referenceLabel,
  rubricOptions,
  type RubricGroup,
} from '../utils/conceptReferences';
import { pageRange } from '../utils/pageRange';
import { decodeRubricPath, encodeRubricPath } from '../utils/rubricPathParam';
import { parseVolumeFilter, volumeFilterValue } from '../utils/volumeFilter';
import { pagePath } from '../utils/paths';
import './ConceptArticleBlock.css';

interface ConceptArticleBlockProps {
  article: ConceptArticle;
  /** Старый плоский фильтр из `?rubric=` — чужие закладки. */
  rubric: string;
  /** Новый фильтр из `?rubric_path=`; непустой путь побеждает плоский. */
  rubricPath: string[];
  volume: string;
  onFilterChange: (next: { rubric: string; rubricPath: string[]; volume: string }) => void;
  /**
   * Идентификаторы адресов (`reference_id`), уже пришедших в поток. Определяет,
   * ведёт ли разрешённый адрес на якорь записи внутри документа или на
   * страницу тома — иначе клик по адресу вне загруженного окна упирался бы в
   * несуществующий якорь.
   */
  loadedRefs: ReadonlySet<number>;
  /**
   * Метка издания. Одной статьи достаточно понять, откуда она, по заголовку
   * страницы — метка нужна только когда статей несколько.
   */
  showEdition?: boolean;
}

/** Тома, встреченные в адресах: значение фильтра и подпись. */
function volumesOf(refs: ConceptReference[]): { value: string; label: string }[] {
  const out: { value: string; label: string }[] = [];
  for (const ref of refs) {
    const value = volumeFilterValue(ref.volume_number, ref.volume_part);
    if (out.some((v) => v.value === value)) continue;
    out.push({
      value,
      label: ref.volume_part
        ? `т. ${ref.volume_number}, ${ref.volume_part}`
        : `т. ${ref.volume_number}`,
    });
  }
  return out;
}

// Значение для пришедшей плоской закладки (`?rubric=`, путь пуст). Не может
// совпасть ни с одним результатом encodeRubricPath: тот кодирует каждое
// звено encodeURIComponent, а управляющие символы он не оставляет как есть.
const FLAT_BOOKMARK = '\u0000flat';

/**
 * Отбор адреса под фильтр. Путь побеждает плоское имя — ветка else, а не два
 * независимых условия: правило названо кодом.
 *
 * Плоская ветка оставлена нетронутой намеренно. По ней приходят чужие
 * закладки со старым `?rubric=`, и выбор листа обязан по-прежнему зачерпывать
 * его из всех родителей — то же поведение, что было до вложенности.
 */
function matchesFilter(
  ref: ConceptReference,
  rubric: string,
  rubricPath: string[],
  volume: string,
): boolean {
  if (rubricPath.length > 0) {
    if (!hasPathPrefix(pathOf(ref), rubricPath)) return false;
  } else if (rubric && ref.rubric !== rubric) {
    return false;
  }
  const parsed = parseVolumeFilter(volume);
  if (!parsed) return true;
  if (ref.volume_number !== parsed.volume) return false;
  return (ref.volume_part ?? '') === (parsed.part ?? '');
}

/**
 * Одна статья указателя о понятии: метка издания (когда статей несколько),
 * оглавление подрубрик, фильтры, адреса, печатный текст, исходящие отсылки и
 * ссылка на скан самой статьи (или на внешний источник, если скана нет).
 *
 * Фильтр применяется и здесь, и к потоку: иначе в оглавлении остались бы
 * пункты, ведущие в никуда.
 */
export const ConceptArticleBlock: React.FC<ConceptArticleBlockProps> = ({
  article,
  rubric,
  rubricPath,
  volume,
  onFilterChange,
  loadedRefs,
  showEdition = false,
}) => {
  const all = article.references;
  const shown = all.filter((ref) => matchesFilter(ref, rubric, rubricPath, volume));
  const groups = groupByRubric(shown);
  const filtered = rubric !== '' || rubricPath.length > 0 || volume !== '';
  // Пункт оглавления — фильтр по подрубрике, а не якорь: сужаются И адреса
  // здесь, И поток справа. Якорем внутри панели он прыгал только слева, а
  // поток оставался на месте — подрубрика могла начинаться с 192-й записи из
  // 247 при порциях по 20, и прыгать справа было не к чему, а догружать до
  // неё на статье в 3479 адресов значит десятки мегабайт.
  //
  // Оглавление поэтому строится с учётом ТОЛЬКО фильтра тома: сужай его ещё
  // и фильтр подрубрики — после первого клика в нём оставался бы один пункт,
  // и к соседней подрубрике пришлось бы идти через «все». Том же отсеивает
  // подрубрики без адресов в нём: такой пункт вёл бы в пустую выдачу.
  // Вложенные подрубрики (аспекты внутри съезда) в оглавление не идут — оно
  // указывает на раздел целиком, а не на каждый лист внутри него.
  const tocGroups = groupByRubric(all.filter((ref) => matchesFilter(ref, '', [], volume))).filter(
    (group) => group.rubric !== '',
  );
  const sourcePages = pageRange(article.source_page_start, article.source_page_end);
  // Оглавление на якорь больше не ссылается (см. tocGroups выше), но id группы
  // остаётся: адреса вида `#rubric-…`, скопированные, пока оно ссылалось,
  // по-прежнему приводят к подрубрике в панели.
  //
  // Якорь несёт id статьи И весь путь, а не только лист: рубрика с
  // одинаковым названием — обычное дело у двух указателей одного понятия
  // («определение» есть почти в каждом), а с задачи 9 ещё и внутри одной
  // статьи — «значение съезда» стоит под семью разными съездами. Без пути
  // все семь получили бы один и тот же id, и ссылка вела бы всегда в первую.
  //
  // Разделитель между элементами пути — «:», а не «-»: encodeURIComponent
  // НЕ кодирует дефис (он в списке «неизбезопасных» символов вместе с
  // `_ . ! ~ * ' ( )`), поэтому путь ["а-б"] (один элемент с дефисом внутри)
  // и путь ["а", "б"] (два элемента) склеивались бы в один и тот же якорь.
  // «:» кодируется всегда (`%3A`), так что литерального «:» внутри
  // закодированного элемента быть не может, и склейка снимается — не тем,
  // что дефис стал безопаснее, а тем, что разделитель ушёл из алфавита,
  // который encodeURIComponent оставляет как есть.
  const rubricAnchor = (path: string[]) =>
    `rubric-${article.id}-${path.map(encodeURIComponent).join(':')}`;

  /** Один адрес статьи — общий для верхнего уровня и вложенных подрубрик. */
  const renderRefItem = (ref: ConceptReference) => (
    <li key={ref.id}>
      {ref.resolved && ref.work_id !== undefined && ref.page_number !== undefined ? (
        loadedRefs.has(ref.id) ? (
          <a href={`#${entryAnchorId(ref.id)}`}>{referenceLabel(ref)}</a>
        ) : (
          // Запись ещё не пришла в поток — якоря в документе нет, ведём на
          // страницу тома, как до этой ветки.
          <Link to={pagePath({ id: ref.work_id, slug: ref.work_slug }, ref.page_number)}>
            {referenceLabel(ref)}
          </Link>
        )
      ) : (
        <>
          <span className="concept-ref-unresolved">{referenceLabel(ref)}</span>{' '}
          <span className="concept-ref-note">— том не загружен</span>
        </>
      )}
      {ref.is_uncertain && (
        <span
          className="concept-ref-uncertain"
          title={ref.note}
          aria-label={ref.note ? `Адрес сомнителен: ${ref.note}` : 'Адрес сомнителен'}
        >
          {' '}
          ⚠
        </span>
      )}
    </li>
  );

  /**
   * Группа подрубрики: своя шапка, свои адреса (если есть — у съезда их
   * может не быть вовсе, всё ушло в аспекты), затем вложенные подрубрики
   * внутри того же блока — «внутри своего подзаголовка, а не наравне с ним».
   * Рекурсия, а не жёсткие два уровня: глубина в базе сегодня максимум 2, но
   * запрос рекурсивный, и рендер не должен ломаться на 3.
   */
  const renderGroup = (group: RubricGroup, top: boolean): React.ReactNode => (
    <div
      key={group.rubric}
      className={top ? 'concept-rubric' : 'concept-subrubric'}
      id={group.rubric ? rubricAnchor(group.path) : undefined}
    >
      {top ? <h3>{group.rubric}</h3> : <h4>{group.rubric}</h4>}
      {group.refs.length > 0 && (
        <ul className="concept-ref-list">{group.refs.map(renderRefItem)}</ul>
      )}
      {group.children.map((child) => renderGroup(child, false))}
    </div>
  );

  return (
    <section className="concept-article-block">
      {showEdition && article.edition_title !== '' && (
        <p className="concept-article-edition">{article.edition_title}</p>
      )}

      {/* Без work_id ссылки нет вовсе: на внешний сайт, с которого снят
          указатель, читальня не ссылается (source_url не показывается). */}
      {article.work_id !== undefined && (
        <Link
          to={pagePath({ id: article.work_id, slug: undefined }, article.source_page_start)}
          className="concept-source-link"
        >
          Указатель, стр. {sourcePages}
        </Link>
      )}

      {tocGroups.length > 0 && (
        <nav className="concept-rubric-toc" aria-label="Подрубрики">
          <ul>
            {tocGroups.map((group) => {
              // Выбран ровно этот раздел — повторный клик снимает фильтр.
              // Плоская закладка (?rubric=) выбранным пунктом не считается:
              // она зачерпывает лист из всех родителей, и клик заменяет её
              // путём — тем же правилом, что у выбора в списке.
              const pressed =
                rubricPath.length === group.path.length && hasPathPrefix(rubricPath, group.path);
              return (
                <li key={group.rubric}>
                  <button
                    type="button"
                    aria-pressed={pressed}
                    onClick={() =>
                      onFilterChange({
                        rubric: '',
                        rubricPath: pressed ? [] : group.path,
                        volume,
                      })
                    }
                  >
                    {group.rubric}
                  </button>
                </li>
              );
            })}
          </ul>
        </nav>
      )}

      {article.kind !== 'redirect' && all.length > 0 && (
        <section className="concept-panel-section">
          <div className="concept-panel-filters">
            <label>
              подрубрика
              <select
                value={
                  rubricPath.length > 0
                    ? encodeRubricPath(rubricPath)
                    : rubric !== ''
                      ? FLAT_BOOKMARK
                      : ''
                }
                onChange={(e) =>
                  onFilterChange({
                    // Выбор из списка всегда путевой: плоское имя остаётся
                    // только у пришедших закладок и при смене фильтра
                    // сбрасывается — иначе два фильтра сужали бы разом.
                    rubric: '',
                    rubricPath:
                      e.target.value === FLAT_BOOKMARK ? [] : decodeRubricPath(e.target.value),
                    volume,
                  })
                }
              >
                <option value="">все</option>
                {/* Пришедшая закладка со старым ?rubric= получает СВОЮ
                    опцию: иначе список показывает «все» при сужённой выдаче,
                    а снять фильтр нажатием «все» нельзя — опция уже
                    выбрана, события change не будет. Опция живёт только
                    пока закладка в силе; выбор любой другой стирает плоское
                    имя путевым. */}
                {rubric !== '' && rubricPath.length === 0 && (
                  <option value={FLAT_BOOKMARK}>{rubric}</option>
                )}
                {rubricOptions(all).map((group, i) =>
                  group.label === '' ? (
                    // Группа без подписи — опции верхнего уровня, без
                    // <optgroup>: так выглядит статья без вложенности, то
                    // есть 2463 статьи из 2464. Fragment, а не голый массив:
                    // ключ обязан висеть на том, что возвращает map, иначе
                    // React считает ключи по месту в списке.
                    <Fragment key={`flat-${i}`}>
                      {group.options.map((option) => (
                        <option
                          key={encodeRubricPath(option.path)}
                          value={encodeRubricPath(option.path)}
                        >
                          {option.label}
                        </option>
                      ))}
                    </Fragment>
                  ) : (
                    <optgroup key={encodeRubricPath([group.label])} label={group.label}>
                      {group.options.map((option) => (
                        <option
                          key={encodeRubricPath(option.path)}
                          value={encodeRubricPath(option.path)}
                        >
                          {option.label}
                        </option>
                      ))}
                    </optgroup>
                  ),
                )}
              </select>
            </label>
            <label>
              том
              <select
                value={volume}
                onChange={(e) => onFilterChange({ rubric, rubricPath, volume: e.target.value })}
              >
                <option value="">все</option>
                {volumesOf(all).map((v) => (
                  <option key={v.value} value={v.value}>
                    {v.label}
                  </option>
                ))}
              </select>
            </label>
          </div>

          <h2>Адреса</h2>
          {groups.map((group) => renderGroup(group, true))}

          <p className="concept-panel-note">
            {filtered
              ? `показано ${shown.length} из ${all.length} адресов`
              : `всего ${all.length} адресов`}
          </p>
        </section>
      )}

      {article.article_markdown !== '' && (
        <section className="concept-panel-section">
          <h2>Статья указателя</h2>
          <div className="concept-panel-article">
            <ReactMarkdown>{article.article_markdown}</ReactMarkdown>
          </div>
        </section>
      )}

      {article.links.length > 0 && (
        <section className="concept-panel-section">
          <h2>Отсылки</h2>
          <ul className="concept-link-list">
            {[...article.links]
              .sort((a, b) => a.order_number - b.order_number)
              .map((link) => (
                <li key={link.id}>
                  <span className="concept-link-kind">
                    {link.kind === 'see' ? 'см.' : 'см. также'}
                  </span>{' '}
                  {link.target_slug ? (
                    <Link to={`/concepts/${link.target_slug}`}>{link.target_title}</Link>
                  ) : (
                    // Цель ещё не разобрана или лежит во втором указателе.
                    <span>{link.target_title}</span>
                  )}
                </li>
              ))}
          </ul>
        </section>
      )}
    </section>
  );
};
