import { useMemo, useRef } from 'react';
import { Link } from 'react-router-dom';
import type { Chapter, Edition, PageMapEntry, Work } from '../types';
import type { LastRead } from '../hooks/useReadingProgress';
import { volumeCoordinates } from '../utils/volumeCoordinates';
import { plural, worksPlural } from '../utils/volumeLabel';
import { worksLevel } from '../utils/volumeOutline';
import { DownloadMenu } from './DownloadMenu';
import { ShareButton } from './ShareButton';
import { FeatureHint } from './FeatureHint';
import { chapterPath, editionPath, pagePath, workPath } from '../utils/paths';
import './VolumeMasthead.css';

interface Props {
  work: Work;
  /** Собрание: только подпись к координатам тома, может не загрузиться. */
  edition: Edition | null;
  chapters: Chapter[];
  pages: PageMapEntry[];
  lastRead: LastRead | null;
  /** Есть звук — пункт .m3u в меню выгрузки. */
  hasAudio?: boolean;
}

export const VolumeMasthead: React.FC<Props> = ({
  work,
  edition,
  chapters,
  pages,
  lastRead,
  hasAudio = false,
}) => {
  // Работы считаются по уровню работ тома, а не по длине массива верхних глав:
  // в томе, где верхний уровень — одна обёртка на весь том, «1 работа» было бы
  // неправдой. Правило не зависит от того, на какой глубине читатель сейчас
  // смотрит содержание: шапка описывает том, а не вид.
  const works = useMemo(() => worksLevel(chapters), [chapters]);

  const firstChapter = chapters[0];
  const startHref = firstChapter
    ? chapterPath(work, firstChapter)
    : pagePath(work, pages[0]?.page_number ?? 1);

  const resume = lastRead && lastRead.workId === work.id ? lastRead : null;
  const children = work.children ?? [];

  // «Читать с начала» не нуждается в карте страниц — адрес считается из
  // первой главы, а на страницу №1 ссылка работает и без карты. Кнопки не
  // должны пропадать, когда карта ещё грузится или вовсе не загрузилась:
  // это единственное главное действие крышки. Строка с числами карту
  // показывает буквально, поэтому ей нужна именно она.
  const canStartReading = chapters.length > 0 || pages.length > 0;
  const frontRef = useRef<HTMLParagraphElement>(null);

  return (
    <header className="vol-masthead">
      {work.edition_id !== undefined && work.edition_id !== null && (
        <p className="vol-masthead-eyebrow">
          <Link to={editionPath({ id: work.edition_id, url_slug: edition?.url_slug })}>
            {edition ? `${edition.title} · ${volumeCoordinates(work)}` : volumeCoordinates(work)}
          </Link>
        </p>
      )}

      <h1 className="vol-masthead-title">{work.title}</h1>

      {work.description?.trim() && (
        // Текстом, не HTML: описание — свободный ввод редактора, а не
        // разметка. Переносы строк сохраняются CSS (white-space), а не
        // вставкой <br /> — тогда textContent совпадает с тем, что редактор
        // напечатал в форме.
        <p className="vol-masthead-description">{work.description}</p>
      )}

      {pages.length > 0 && (
        <p className="vol-masthead-figures">
          {worksPlural(works.length)} · {plural(pages.length, ['страница', 'страницы', 'страниц'])}
        </p>
      )}

      {canStartReading && (
        <div className="vol-masthead-actions">
          <Link to={startHref} className="vol-masthead-start">
            Читать с начала
          </Link>
          {resume && (
            <Link
              // Слаг главы «Продолжить» не хранится в localStorage (см.
              // LastRead) — числовой сегмент здесь законен, а не запасной
              // вариант.
              to={chapterPath(work, { id: resume.chapterId })}
              className="vol-masthead-resume"
            >
              Продолжить: {resume.chapterTitle}
              {resume.pageNumber !== null && `, стр. ${resume.pageNumber}`}
            </Link>
          )}
          <DownloadMenu href={`/api/works/${work.id}/download`} audio={hasAudio} />
          {/* Подпись — та же, что во вкладке (WorkDetail, useDocumentTitle):
              название вперёд, автор следом. */}
          <ShareButton
            title={[work.title, work.author].filter(Boolean).join(' — ')}
            path={workPath(work)}
            className="vol-masthead-share"
          />
        </div>
      )}

      {children.length > 0 && (
        <>
          <p className="vol-masthead-front" ref={frontRef}>
            Предваряющие материалы:{' '}
            {children.map((child, i) => (
              <span key={child.id}>
                {i > 0 && ' · '}
                <Link to={workPath(child)}>{child.title}</Link>
              </span>
            ))}
          </p>
          <FeatureHint id="front-matter" anchorRef={frontRef} />
        </>
      )}
    </header>
  );
};
