import { useState, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  DndContext,
  DragOverlay,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  DragStartEvent,
  DragEndEvent,
} from '@dnd-kit/core';
import {
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
  useSortable,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import type { Chapter, Work } from '../types';
import { findChapterById, flattenChapters } from '../utils/chapterTree';
import { chapterTypeLabel } from '../utils/chapterTypeLabel';
import { plural } from '../utils/volumeLabel';
import { chapterPath } from '../utils/paths';
import './ChapterTree.css';

interface ChapterTreeProps {
  chapters: Chapter[];
  work: Work;
  canEdit: boolean;
  onMove: (chapterId: number, parentId: number | null, orderNumber: number) => Promise<void>;
  onDelete: (chapterId: number) => void;
}

interface ChapterNodeProps {
  chapter: Chapter;
  work: Work;
  canEdit: boolean;
  depth: number;
  isExpanded: boolean;
  onToggleExpand: (chapterId: number) => void;
  onDelete: (chapterId: number) => void;
  isOverlay?: boolean;
}

function getParentId(chapters: Chapter[], chapterId: number): number | null {
  function search(
    items: Chapter[],
    parentId: number | null,
  ): { found: boolean; parentId: number | null } {
    for (const chapter of items) {
      if (chapter.id === chapterId) {
        return { found: true, parentId };
      }
      if (chapter.children && chapter.children.length > 0) {
        const result = search(chapter.children, chapter.id);
        if (result.found) return result;
      }
    }
    return { found: false, parentId: null };
  }
  return search(chapters, null).parentId;
}

const SortableChapterNode: React.FC<ChapterNodeProps> = ({
  chapter,
  work,
  canEdit,
  depth,
  isExpanded,
  onToggleExpand,
  onDelete,
  isOverlay = false,
}) => {
  const navigate = useNavigate();
  const hasChildren = chapter.children && chapter.children.length > 0;

  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: chapter.id,
    disabled: !canEdit,
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
    marginLeft: `${depth * 24}px`,
  };

  const overlayStyle = isOverlay ? { marginLeft: 0 } : {};

  return (
    <div
      ref={setNodeRef}
      style={{ ...style, ...overlayStyle }}
      className={`chapter-tree-node ${isDragging ? 'dragging' : ''} ${isOverlay ? 'overlay' : ''}`}
    >
      <div className="chapter-tree-card">
        {canEdit && (
          <div className="drag-handle" {...attributes} {...listeners}>
            <span className="drag-icon">⋮⋮</span>
          </div>
        )}

        <div className="chapter-tree-expand">
          {hasChildren ? (
            <button
              className="expand-button"
              onClick={() => onToggleExpand(chapter.id)}
              title={isExpanded ? 'Свернуть' : 'Развернуть'}
            >
              {isExpanded ? '▼' : '▶'}
            </button>
          ) : (
            <span className="expand-placeholder" />
          )}
        </div>

        <div className="chapter-tree-info">
          <div className="chapter-tree-title">{chapter.title}</div>
          <div className="chapter-tree-meta">
            <span className="chapter-type-badge">{chapterTypeLabel(chapter.type)}</span>
            <span className="chapter-pages-info">
              Страницы {chapter.start_page}—{chapter.end_page}
            </span>
            {hasChildren && (
              <span className="chapter-children-count">
                {plural(chapter.children!.length, ['подглава', 'подглавы', 'подглав'])}
              </span>
            )}
          </div>
        </div>

        <div className="chapter-tree-actions">
          <button
            onClick={() => navigate(chapterPath(work, chapter))}
            className="chapter-tree-btn view"
          >
            Открыть
          </button>
          {canEdit && (
            <>
              <button
                onClick={() => navigate(`${chapterPath(work, chapter)}/edit`)}
                className="chapter-tree-btn edit"
              >
                Изменить
              </button>
              <button onClick={() => onDelete(chapter.id)} className="chapter-tree-btn delete">
                Удалить
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
};

export const ChapterTree: React.FC<ChapterTreeProps> = ({
  chapters,
  work,
  canEdit,
  onMove,
  onDelete,
}) => {
  const [expandedIds, setExpandedIds] = useState<Set<number>>(new Set());
  const [activeId, setActiveId] = useState<number | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 8,
      },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  const toggleExpand = (chapterId: number) => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(chapterId)) {
        next.delete(chapterId);
      } else {
        next.add(chapterId);
      }
      return next;
    });
  };

  const expandAll = () => {
    const allIds = flattenChapters(chapters)
      .filter((c) => c.children && c.children.length > 0)
      .map((c) => c.id);
    setExpandedIds(new Set(allIds));
  };

  const collapseAll = () => {
    setExpandedIds(new Set());
  };

  const visibleChapters = useMemo(() => {
    const result: { chapter: Chapter; depth: number }[] = [];

    function traverse(items: Chapter[], depth: number) {
      for (const item of items) {
        result.push({ chapter: item, depth });
        if (item.children && item.children.length > 0 && expandedIds.has(item.id)) {
          traverse(item.children, depth + 1);
        }
      }
    }

    traverse(chapters, 0);
    return result;
  }, [chapters, expandedIds]);

  const sortableIds = useMemo(
    () => visibleChapters.map(({ chapter }) => chapter.id),
    [visibleChapters],
  );

  const handleDragStart = (event: DragStartEvent) => {
    setActiveId(event.active.id as number);
  };

  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event;
    setActiveId(null);

    if (!over || active.id === over.id) return;

    const activeChapter = findChapterById(chapters, active.id as number);
    const overChapter = findChapterById(chapters, over.id as number);

    if (!activeChapter || !overChapter) return;

    const overParentId = getParentId(chapters, over.id as number);
    await onMove(activeChapter.id, overParentId, overChapter.order_number);
  };

  const activeChapter = activeId ? findChapterById(chapters, activeId) : null;

  if (chapters.length === 0) {
    return (
      <div className="chapter-tree-empty">
        <p>Глав пока нет.</p>
      </div>
    );
  }

  return (
    <div className="chapter-tree-container">
      <div className="chapter-tree-toolbar">
        <button onClick={expandAll} className="toolbar-btn">
          Развернуть все
        </button>
        <button onClick={collapseAll} className="toolbar-btn">
          Свернуть все
        </button>
        {canEdit && (
          <span className="toolbar-hint">Перетаскивайте главы, чтобы изменить порядок</span>
        )}
      </div>

      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
      >
        <SortableContext items={sortableIds} strategy={verticalListSortingStrategy}>
          <div className="chapter-tree-list">
            {visibleChapters.map(({ chapter, depth }) => (
              <SortableChapterNode
                key={chapter.id}
                chapter={chapter}
                work={work}
                canEdit={canEdit}
                depth={depth}
                isExpanded={expandedIds.has(chapter.id)}
                onToggleExpand={toggleExpand}
                onDelete={onDelete}
              />
            ))}
          </div>
        </SortableContext>

        <DragOverlay>
          {activeChapter ? (
            <SortableChapterNode
              chapter={activeChapter}
              work={work}
              canEdit={canEdit}
              depth={0}
              isExpanded={false}
              onToggleExpand={() => {}}
              onDelete={() => {}}
              isOverlay
            />
          ) : null}
        </DragOverlay>
      </DndContext>
    </div>
  );
};
