import { useCallback, useEffect, useMemo, useState } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import toast from 'react-hot-toast';
import { worksApi, chaptersApi, editionsApi } from '../services/api';
import { useAuth, canEdit } from '../hooks/useAuth';
import { useCanonicalPath } from '../hooks/useCanonicalPath';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { VolumeMasthead } from '../components/VolumeMasthead';
import { VolumeScale } from '../components/VolumeScale';
import { VolumeOutline } from '../components/VolumeOutline';
import { VolumeManagePanel } from '../components/VolumeManagePanel';
import { VolumeAudio } from '../components/VolumeAudio';
import { useWorkAudio } from '../hooks/useWorkAudio';
import { SearchTrigger } from '../components/SearchTrigger';
import { readRecent, type LastRead } from '../hooks/useReadingProgress';
import type { Work, Chapter, Edition, PageMapEntry, PageStatus } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { numericId, workPath } from '../utils/paths';
import './WorkDetail.css';

export const WorkDetail: React.FC = () => {
  const { id: idParam } = useParams<{ id: string }>();
  const id = numericId(idParam);
  const navigate = useNavigate();
  const { user } = useAuth();

  const [work, setWork] = useState<Work | null>(null);
  const [chapters, setChapters] = useState<Chapter[]>([]);
  const [pageMap, setPageMap] = useState<PageMapEntry[]>([]);
  // Карта страниц грузится отдельным запросом (см. loadWorkDetail) и может
  // ещё лететь, когда работа и главы уже пришли. 'loading' отличает «ещё не
  // знаем» от «пришла пустой», иначе том мигает надписью «ещё не расписана»,
  // пока запрос летит.
  const [pageMapStatus, setPageMapStatus] = useState<'loading' | 'ok' | 'failed'>('loading');
  const [edition, setEdition] = useState<Edition | null>(null);
  // Список, а не одна запись: шапка ищет в нём свежее место этого тома, и
  // чтение другого тома его больше не затирает.
  const [recent] = useState<LastRead[]>(() => readRecent());
  const [isLoading, setIsLoading] = useState(true);
  const [isUploading, setIsUploading] = useState(false);
  const [isCreatingPages, setIsCreatingPages] = useState(false);
  const [highlight, setHighlight] = useState<PageStatus | null>(null);
  const [reordering, setReordering] = useState(false);
  const [error, setError] = useState('');
  const { audio, reload: reloadAudio } = useWorkAudio(id);
  const hasAudio = !!audio && (audio.tracks.length > 0 || audio.recordings.length > 0);

  // Различающее — название — вперёд, автор следом: в этом собрании 45 томов
  // одного автора, а вкладка и сниппет поисковика обрезают конец строки.
  // Порядок и разделитель — те же, что печатает internal/seo/render_volume.go
  // (Work), иначе закладка на том снова была бы неотличима от закладки на
  // главу — та самая беда, ради которой существует этот заголовок.
  useDocumentTitle(work ? [work.title, work.author].filter(Boolean).join(' — ') : null);

  // Заход по старой числовой ссылке (/works/49) тихо подменяется каноном
  // (/works/49-lenin-t06), как только слаг стал известен из ответа сервера.
  //
  // Сравнение с `work.id === id` обязательно: WorkDetail не размонтируется
  // при переходе на другую работу (например, по ссылке «Предваряющие
  // материалы» — тот же маршрут, меняется только :id), и на один тик между
  // сменой адреса и ответом сервера `work` ещё несёт данные прежней работы.
  // Без сравнения канон строился бы по ней и совпадал со старым адресом, а
  // не с новым — эффект тут же откатывал бы адрес обратно.
  useCanonicalPath(work && work.id === id ? workPath(work) : null);

  // Переход на другую работу (например, по ссылке «Предваряющие материалы»)
  // не должен тащить подсветку статуса и режим перестановки глав с прежней
  // работы: легенда обреза рисует только статусы с ненулевым счётчиком, и
  // если в новой работе такого статуса нет, подсветка гасит всё содержание,
  // а снять её кнопкой неоткуда — легенда для неё не нарисована.
  const [prevId, setPrevId] = useState(id);
  if (prevId !== id) {
    setPrevId(id);
    setHighlight(null);
    setReordering(false);
  }

  const loadWorkDetail = useCallback(async () => {
    // Битый сегмент id (numericId вернул null): грузить нечего, но флаг
    // загрузки обязан сброситься — иначе карточка вечно висит на «Загружаем
    // том…» вместо уже существующего экрана «Том не найден» ниже.
    if (id === null) {
      setIsLoading(false);
      return;
    }

    try {
      const [workResponse, chaptersResponse] = await Promise.all([
        worksApi.get(id),
        chaptersApi.list(id),
      ]);
      setWork(workResponse.data);
      setChapters(chaptersResponse.data || []);
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось загрузить том'));
    } finally {
      setIsLoading(false);
    }

    // Карта страниц — отдельным запросом со своим catch: без неё обрез и
    // клетки не рисуются, но содержание и крышка тома работают.
    try {
      const mapResponse = await worksApi.pageMap(id);
      setPageMap(mapResponse.data || []);
      setPageMapStatus('ok');
    } catch {
      setPageMap([]);
      setPageMapStatus('failed');
    }
  }, [id]);

  useEffect(() => {
    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadWorkDetail();
  }, [loadWorkDetail]);

  const editionId = work?.edition_id;

  // Смена/пропажа edition_id должна сразу сбросить прежнее название собрания,
  // а не показывать его до ответа нового запроса.
  const [prevEditionId, setPrevEditionId] = useState(editionId);
  if (prevEditionId !== editionId) {
    setPrevEditionId(editionId);
    setEdition(null);
  }

  useEffect(() => {
    // Название собрания — только подпись к координатам тома; падение этого
    // запроса не должно ломать саму карточку.
    if (editionId === undefined || editionId === null) return;
    let cancelled = false;
    editionsApi
      .get(editionId)
      .then((response) => {
        if (!cancelled) setEdition(response.data);
      })
      .catch(() => {
        // Ссылка на собрание останется без названия — координаты тома видны
        // и без него.
      });
    return () => {
      cancelled = true;
    };
  }, [editionId]);

  const editable = canEdit(user);

  const handleDeleteWork = async () => {
    if (!work) return;
    try {
      await worksApi.delete(work.id);
      toast.success('Том удалён');
      navigate('/');
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось удалить том'));
    }
  };

  const handleUpload = async (file: File) => {
    if (!work) return;

    const validTypes = ['application/pdf', 'image/vnd.djvu', 'image/x-djvu'];
    if (!validTypes.includes(file.type) && !file.name.endsWith('.djvu')) {
      setError('Нужен файл PDF или DJVU');
      return;
    }

    setIsUploading(true);
    setError('');
    try {
      const formData = new FormData();
      formData.append('file', file);
      await worksApi.uploadFile(work.id, formData);
      toast.success('Файл загружен');
      void loadWorkDetail();
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось загрузить файл'));
    } finally {
      setIsUploading(false);
    }
  };

  // Возвращает признак успеха: панель очищает поле диапазона только когда
  // страницы действительно создались, а не при каждом нажатии кнопки.
  const handleCreatePages = async (pageRange: string): Promise<boolean> => {
    if (!work) return false;

    setIsCreatingPages(true);
    setError('');
    try {
      const response = await worksApi.createPages(work.id, pageRange || undefined);
      toast.success(`Создано страниц: ${response.data.created_pages}`);
      void loadWorkDetail();
      return true;
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось создать страницы'));
      return false;
    } finally {
      setIsCreatingPages(false);
    }
  };

  const handleDeleteChapter = async (chapterId: number) => {
    if (!work || !window.confirm('Удалить главу?')) return;
    try {
      await chaptersApi.delete(work.id, chapterId);
      toast.success('Глава удалена');
      void loadWorkDetail();
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось удалить главу'));
    }
  };

  const handleMoveChapter = async (
    chapterId: number,
    parentId: number | null,
    orderNumber: number,
  ) => {
    if (!work) return;
    try {
      await chaptersApi.move(work.id, chapterId, {
        parent_id: parentId,
        order_number: orderNumber,
      });
      await loadWorkDetail();
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось переставить главу'));
    }
  };

  // Пусто показываем только когда карта действительно пришла пустой — не
  // пока она ещё грузится и не когда запрос вовсе провалился (тогда есть
  // свой блок vol-map-error, а содержание может остаться на прежних данных).
  const emptyVolume = useMemo(
    () => pageMapStatus === 'ok' && pageMap.length === 0,
    [pageMapStatus, pageMap.length],
  );

  if (isLoading) {
    return <div className="loading-state">Загружаем том…</div>;
  }

  if (!work) {
    return (
      <div className="error-state">
        <div>{error || 'Том не найден'}</div>
        <Link to="/" className="back-link">
          ← К собранию
        </Link>
      </div>
    );
  }

  return (
    <div className="work-detail-container">
      <Link to="/" className="back-link">
        ← К собранию
      </Link>

      {error && <div className="error-message">{error}</div>}

      <VolumeMasthead
        work={work}
        edition={edition}
        chapters={chapters}
        pages={pageMap}
        lastRead={recent.find((e) => e.workId === work.id) ?? null}
        hasAudio={hasAudio}
      />

      {!emptyVolume && (
        <div className="work-detail-search">
          <SearchTrigger
            scope={{ editions: [], works: [work.id], chapters: [] }}
            label="Искать в томе"
          />
        </div>
      )}

      {pageMapStatus === 'failed' && (
        <p className="vol-map-error">Карту страниц не удалось загрузить</p>
      )}

      {emptyVolume ? (
        <div className="vol-empty">
          <p>
            {editable
              ? 'В томе ещё нет страниц. Загрузите файл и создайте их в панели управления.'
              : 'Том ещё не расписан по страницам.'}
          </p>
        </div>
      ) : (
        <>
          {/* Оглавление первым: открыв том, читатель ищет, что в нём
              написано. Обрез тома отвечает на другой вопрос — в каком
              состоянии полосы — и потому стоит под содержанием. Подсветка
              статуса из легенды обреза гасит клетки в карточках выше:
              легенда осталась там же, где и клетки, которыми она правит. */}
          {!reordering && (
            <VolumeOutline
              work={work}
              chapters={chapters}
              pages={pageMap}
              editable={editable}
              highlight={highlight}
            />
          )}

          <VolumeScale
            work={work}
            pages={pageMap}
            chapters={chapters}
            editable={editable}
            highlight={highlight}
            onHighlight={setHighlight}
          />
        </>
      )}

      {audio && (hasAudio || editable) && (
        <VolumeAudio
          work={work}
          audio={audio}
          chapters={chapters}
          editable={editable}
          onChanged={reloadAudio}
        />
      )}

      {editable && (
        <VolumeManagePanel
          work={work}
          chapters={chapters}
          isUploading={isUploading}
          isCreatingPages={isCreatingPages}
          onUpload={handleUpload}
          onCreatePages={handleCreatePages}
          onDeleteWork={handleDeleteWork}
          onDeleteChapter={handleDeleteChapter}
          onMoveChapter={handleMoveChapter}
          reordering={reordering}
          onReorderingChange={setReordering}
        />
      )}
    </div>
  );
};
