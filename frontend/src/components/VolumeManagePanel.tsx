import { useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import type { Chapter, Work } from '../types';
import { ChapterTree } from './ChapterTree';
import { workPath } from '../utils/paths';
import './VolumeManagePanel.css';

interface Props {
  work: Work;
  chapters: Chapter[];
  isUploading: boolean;
  isCreatingPages: boolean;
  onUpload: (file: File) => void;
  /** Возвращает признак успеха — панель очищает поле диапазона только тогда. */
  onCreatePages: (pageRange: string) => Promise<boolean>;
  onDeleteWork: () => void;
  onDeleteChapter: (chapterId: number) => void;
  onMoveChapter: (chapterId: number, parentId: number | null, orderNumber: number) => Promise<void>;
  reordering: boolean;
  onReorderingChange: (on: boolean) => void;
}

export const VolumeManagePanel: React.FC<Props> = ({
  work,
  chapters,
  isUploading,
  isCreatingPages,
  onUpload,
  onCreatePages,
  onDeleteWork,
  onDeleteChapter,
  onMoveChapter,
  reordering,
  onReorderingChange,
}) => {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [pageRange, setPageRange] = useState('');

  const pickFile = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (file) onUpload(file);
    if (fileInputRef.current) fileInputRef.current.value = '';
  };

  return (
    <details className="vol-manage">
      <summary>Управление томом</summary>

      <div className="vol-manage-body">
        <section className="vol-manage-block">
          <h3>Файл</h3>
          <p className="vol-manage-status">
            {work.file_path ? 'Файл загружен' : 'Файл не загружен'}
          </p>
          <input
            ref={fileInputRef}
            type="file"
            accept=".pdf,.djvu,application/pdf"
            onChange={pickFile}
            disabled={isUploading}
            style={{ display: 'none' }}
          />
          <button
            type="button"
            onClick={() => fileInputRef.current?.click()}
            disabled={isUploading}
          >
            {isUploading ? 'Загружаем…' : work.file_path ? 'Заменить файл' : 'Загрузить PDF/DJVU'}
          </button>
        </section>

        {work.file_path && (
          <section className="vol-manage-block">
            <h3>Страницы</h3>
            <label htmlFor="vol-manage-range">Диапазон страниц</label>
            <input
              id="vol-manage-range"
              type="text"
              value={pageRange}
              onChange={(event) => setPageRange(event.target.value)}
              placeholder="1-5,7,10-12 — пусто значит весь том"
            />
            <button
              type="button"
              onClick={async () => {
                const created = await onCreatePages(pageRange);
                if (created) setPageRange('');
              }}
              disabled={isCreatingPages}
            >
              {isCreatingPages ? 'Создаём…' : 'Создать страницы'}
            </button>
          </section>
        )}

        <section className="vol-manage-block">
          <h3>Главы</h3>
          <Link to={`${workPath(work)}/chapters/new`} className="vol-manage-link">
            Добавить главу
          </Link>
          <button type="button" onClick={() => onReorderingChange(!reordering)}>
            {reordering ? 'Закончить перестановку' : 'Изменить порядок глав'}
          </button>
          {/* Дерево живёт внутри панели: перетаскивание, правка и удаление глав
              нужны редактору и мешают чтению. Компонент — прежний. */}
          {reordering && (
            <ChapterTree
              chapters={chapters}
              work={work}
              canEdit
              onMove={onMoveChapter}
              onDelete={onDeleteChapter}
            />
          )}
        </section>

        <section className="vol-manage-block">
          <h3>Том</h3>
          <Link to={`${workPath(work)}/edit`} className="vol-manage-link">
            Изменить сведения
          </Link>
          <button
            type="button"
            className="vol-manage-danger"
            onClick={() => {
              if (window.confirm(`Удалить том «${work.title}» со всеми страницами?`)) {
                onDeleteWork();
              }
            }}
          >
            Удалить том
          </button>
        </section>
      </div>
    </details>
  );
};
