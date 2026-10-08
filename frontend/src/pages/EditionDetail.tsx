import { useEffect, useState } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import toast from 'react-hot-toast';
import { editionsApi } from '../services/api';
import { useAuth, canEdit } from '../hooks/useAuth';
import { useCanonicalPath } from '../hooks/useCanonicalPath';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import type { Edition, EditionHighlight, VolumeSummary } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { readVolumeView, writeVolumeView, type VolumeView } from '../utils/volumeViewPref';
import { splitEditionVolumes } from '../utils/editionStats';
import { EditionHighlights } from '../components/EditionHighlights';
import { EditionSummary } from '../components/EditionSummary';
import { EditionPrefaces } from '../components/EditionPrefaces';
import { SearchTrigger } from '../components/SearchTrigger';
import { ShareButton } from '../components/ShareButton';
import { VolumeShelf } from '../components/VolumeShelf';
import { VolumeTable } from '../components/VolumeTable';
import { editionPath, numericId } from '../utils/paths';
import './EditionDetail.css';

export const EditionDetail: React.FC = () => {
  const { id: idParam } = useParams<{ id: string }>();
  const id = numericId(idParam);
  const navigate = useNavigate();
  const { user } = useAuth();
  const editable = canEdit(user);
  const [edition, setEdition] = useState<Edition | null>(null);
  // Маршрут отдаёт VolumeSummary с агрегатами — раньше тип был сужен до Work,
  // и все они выбрасывались.
  const [volumes, setVolumes] = useState<VolumeSummary[]>([]);
  const [highlights, setHighlights] = useState<EditionHighlight[]>([]);
  const [view, setView] = useState<VolumeView>(() => readVolumeView());
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');

  useDocumentTitle(edition?.title);

  // Заход по старой числовой ссылке тихо подменяется каноном, как только
  // собрание приехало и слаг стал известен.
  //
  // Сравнение с `edition.id === id` — та же защита, что и в WorkDetail:
  // EditionDetail не размонтируется при смене :id, и на один тик между
  // сменой адреса и ответом сервера `edition` ещё несёт данные прежнего
  // собрания — без сравнения канон откатывал бы новый адрес на старый.
  useCanonicalPath(edition && edition.id === id ? editionPath(edition) : null);

  // Предваряющие работы (предисловие ко всему собранию или его части) не
  // несут номера тома и не входят в сводку — иначе счётчик у собрания
  // Маркса стал бы 52 тома вместо 50.
  const { prefaces, shelf } = splitEditionVolumes(volumes);

  useEffect(() => {
    const load = async () => {
      // Битый сегмент id (numericId вернул null): грузить нечего, но флаг
      // загрузки обязан сброситься — иначе карточка вечно висит на «Загрузка
      // собрания…» вместо уже существующего экрана «Собрание не найдено» ниже.
      // Сброс — внутри асинхронной функции, а не прямо в теле эффекта:
      // react-hooks/set-state-in-effect запрещает синхронный setState там.
      if (id === null) {
        setIsLoading(false);
        return;
      }

      try {
        const [editionResponse, worksResponse] = await Promise.all([
          editionsApi.get(id),
          editionsApi.volumes(id),
        ]);
        setEdition(editionResponse.data);
        // Порядок задаёт сервер (volume_number NULLS LAST, volume_part,
        // title) — клиент не пересортировывает.
        setVolumes(worksResponse.data ?? []);
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Не удалось загрузить собрание'));
      } finally {
        setIsLoading(false);
      }
    };

    // Избранное грузится отдельно и молча: это украшение страницы, а не её
    // содержание. Бэкенд старше этой работы отвечает 404, и собрание обязано
    // показаться без строки избранного, а не с ошибкой вместо полки.
    const loadHighlights = async () => {
      if (id === null) return;
      try {
        const { data } = await editionsApi.highlights(id);
        setHighlights(data ?? []);
      } catch {
        setHighlights([]);
      }
    };

    // Загрузчики забирают свои ошибки во внутреннем catch и не реджектятся,
    // поэтому промисы здесь честно отбрасываются, а не проглатываются.
    void load();
    void loadHighlights();
  }, [id]);

  const chooseView = (next: VolumeView) => {
    setView(next);
    writeVolumeView(next);
  };

  const handleDelete = async () => {
    if (!edition) return;
    // Бэкенд удаляет собрание, обнуляя edition_id у томов, — сами работы
    // остаются. Сказать это вслух важнее обычного: «удалить собрание»
    // читается как «удалить тома». Исключение — предваряющая работа
    // (edition_front_matter): для неё edition_id обязателен по CHECK, и
    // попытка обнулить его при удалении собрания провалит саму операцию —
    // такое собрание сначала нужно избавить от предваряющей работы.
    const confirmed = window.confirm(
      `Удалить собрание «${edition.title}»? Работы останутся, но потеряют привязку к собранию, ` +
        'и ссылки указателя на их тома перестанут открываться.',
    );
    if (!confirmed) return;

    try {
      await editionsApi.remove(edition.id);
      toast.success('Собрание удалено');
      navigate('/');
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось удалить собрание'));
    }
  };

  if (isLoading) {
    return <div className="loading-state">Загрузка собрания…</div>;
  }

  if (error || !edition) {
    return (
      <div className="error-state">
        <div>{error || 'Собрание не найдено'}</div>
        <Link to="/" className="back-link">
          ← В читальню
        </Link>
      </div>
    );
  }

  return (
    <div className="edition-list-container">
      <Link to="/" className="back-link">
        ← В читальню
      </Link>

      <EditionSummary edition={edition} volumes={shelf} />

      {/* Не в EditionSummary: та же сводка стоит на главной, по одной на
          собрание, и пять «Поделиться» подряд там ни к чему. */}
      <div className="edition-share">
        <ShareButton title={edition.title} path={editionPath(edition)} />
      </div>

      {/* Избранные работы собрания — прямо на главу, минуя том: новичку это
          точка входа. Строку правит редактор на экране «Избранное»; на
          главной её нет. */}
      <EditionHighlights highlights={highlights} />

      {shelf.length > 0 && (
        <div className="edition-search">
          <SearchTrigger
            scope={{ editions: [edition.id], works: [], chapters: [] }}
            label="Искать в собрании"
          />
        </div>
      )}

      <EditionPrefaces prefaces={prefaces} />

      {/* Вынесено из-под shelf.length > 0: у пустого собрания редактору
          нужна кнопка «Добавить том» — иначе первый том в него с этого
          экрана не добавить. Переключатель вида без томов показывать
          нечего, а панель редактора нужна и на пустом собрании. */}
      {(shelf.length > 0 || editable) && (
        <div className="edition-toolbar">
          {shelf.length > 0 && (
            <div className="edition-view-switch" role="group" aria-label="Вид списка томов">
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                aria-pressed={view === 'spines'}
                onClick={() => chooseView('spines')}
              >
                Корешки
              </button>
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                aria-pressed={view === 'list'}
                onClick={() => chooseView('list')}
              >
                Список
              </button>
            </div>
          )}

          {editable && (
            <div className="edition-actions">
              <Link to="/works/new" className="btn btn-primary btn-sm">
                Добавить том
              </Link>
              <Link to={`${editionPath(edition)}/edit`} className="btn btn-secondary btn-sm">
                Изменить
              </Link>
              <Link to={`${editionPath(edition)}/highlights`} className="btn btn-secondary btn-sm">
                Избранное
              </Link>
              <button
                type="button"
                className="btn btn-danger btn-sm"
                onClick={() => void handleDelete()}
              >
                Удалить
              </button>
            </div>
          )}
        </div>
      )}

      {shelf.length === 0 ? (
        <div className="empty-state">
          <p>В собрании пока нет работ.</p>
        </div>
      ) : view === 'spines' ? (
        <VolumeShelf volumes={shelf} volumesPlanned={edition.volumes_planned} />
      ) : (
        <VolumeTable volumes={shelf} />
      )}
    </div>
  );
};
