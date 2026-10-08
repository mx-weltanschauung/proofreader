import { useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import type { Chapter, PageMapEntry, PageStatus, Work } from '../types';
import {
  PAGE_STATUS_LABEL,
  PAGE_STATUS_MODIFIER,
  PAGE_STATUS_ORDER,
  countByStatus,
} from '../utils/pageStatus';
import { FeatureHint } from './FeatureHint';
import { chapterPath, pagePath } from '../utils/paths';
import { pageAnchorId } from '../utils/pageAnchor';
import { missingPageMessage, parsePrintedPage, resolvePageJump } from '../utils/pageJump';
import './VolumeScale.css';

interface Props {
  work: Work;
  pages: PageMapEntry[];
  /**
   * Дерево глав тома. Начала глав верхнего уровня размечают полосу волосяными
   * границами; по всему дереву переход ищет главу, накрывающую страницу.
   */
  chapters: Chapter[];
  editable: boolean;
  highlight: PageStatus | null;
  onHighlight: (status: PageStatus | null) => void;
}

export const VolumeScale: React.FC<Props> = ({
  work,
  pages,
  chapters,
  editable,
  highlight,
  onHighlight,
}) => {
  const navigate = useNavigate();
  const [jump, setJump] = useState('');
  const [jumpError, setJumpError] = useState('');
  const stripRef = useRef<HTMLDivElement>(null);

  const counts = useMemo(() => countByStatus(pages), [pages]);
  const known = useMemo(() => new Set(pages.map((page) => page.page_number)), [pages]);

  // Начало работы и её название на каждую страницу: полоса подписывает клетку
  // при наведении, а начала получают границу.
  const starts = useMemo(() => new Set(chapters.map((c) => c.start_page)), [chapters]);
  const titleByPage = useMemo(() => {
    const byPage = new Map<number, string>();
    for (const chapter of chapters) {
      for (let n = chapter.start_page; n <= chapter.end_page; n += 1) {
        if (!byPage.has(n)) byPage.set(n, chapter.title);
      }
    }
    return byPage;
  }, [chapters]);

  // Номер в поле — печатный, как в ссылке, с которой читатель пришёл.
  // Ведёт он в текст: в самую узкую главу, накрывающую страницу, сразу на
  // её место, — тем же правилом, что и переход из панели чтения. Отдельная
  // полоса со сканом открывается, только если глав над страницей нет
  // (содержание, колофон); сверка со сканом остаётся в панели главы.
  // Редактору поле по-прежнему открывает правку полосы.
  const submitJump = (event: React.FormEvent) => {
    event.preventDefault();
    const target = parsePrintedPage(jump, work);
    if (target === null || !known.has(target)) {
      setJumpError(missingPageMessage(jump));
      return;
    }
    setJumpError('');
    if (editable) {
      navigate(`${pagePath(work, target)}/edit`);
      return;
    }
    const resolved = resolvePageJump(target, chapters, () => false);
    navigate(
      resolved.kind === 'chapter'
        ? `${chapterPath(work, resolved.chapter)}#${pageAnchorId(target)}`
        : pagePath(work, target),
    );
  };

  if (pages.length === 0) return null;

  return (
    <section className="vol-scale" aria-labelledby="vol-scale-title">
      <h2 className="vol-scale-title" id="vol-scale-title">
        Обрез тома
      </h2>

      {/* Полоса — картина, а не список ссылок: при 840 страницах на клетку
          приходится один-два пикселя, и попасть в неё нельзя. Переход по
          страницам живёт в клетках карточек работ и в поле ниже. */}
      <div className="vol-scale-strip" ref={stripRef} role="img" aria-label="Обрез тома">
        {pages.map((entry) => {
          const chapterTitle = titleByPage.get(entry.page_number);
          const dimmed = highlight !== null && entry.status !== highlight;
          return (
            <span
              key={entry.page_number}
              className={[
                'vol-scale-tick',
                `is-${PAGE_STATUS_MODIFIER[entry.status]}`,
                starts.has(entry.page_number) ? 'is-work-start' : '',
                dimmed ? 'is-dimmed' : '',
              ]
                .filter(Boolean)
                .join(' ')}
              title={
                chapterTitle
                  ? `стр. ${entry.page_number} · ${chapterTitle}`
                  : `стр. ${entry.page_number}`
              }
            />
          );
        })}
      </div>
      <FeatureHint id="volume-scale" anchorRef={stripRef} />

      <div className="vol-scale-controls">
        <ul className="vol-scale-legend">
          {PAGE_STATUS_ORDER.filter((status) => counts[status]).map((status) => (
            <li key={status}>
              <button
                type="button"
                className={`vol-scale-key${highlight === status ? ' is-on' : ''}`}
                aria-pressed={highlight === status}
                onClick={() => onHighlight(highlight === status ? null : status)}
              >
                <span
                  className={`vol-scale-swatch is-${PAGE_STATUS_MODIFIER[status]}`}
                  aria-hidden="true"
                />
                {PAGE_STATUS_LABEL[status]}, {counts[status]}
              </button>
            </li>
          ))}
        </ul>

        <form className="vol-scale-jump" onSubmit={submitJump}>
          <label htmlFor="vol-scale-jump-input">Перейти к странице</label>
          <input
            id="vol-scale-jump-input"
            inputMode={work.numbering_style === 'roman' ? 'text' : 'numeric'}
            autoComplete="off"
            value={jump}
            onChange={(event) => {
              setJump(event.target.value);
              setJumpError('');
            }}
          />
          <button type="submit">Перейти</button>
        </form>
      </div>

      {jumpError && (
        <p className="vol-scale-error" role="alert">
          {jumpError}
        </p>
      )}
    </section>
  );
};
