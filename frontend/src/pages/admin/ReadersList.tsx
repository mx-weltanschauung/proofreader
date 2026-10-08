import { Fragment, useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import toast from 'react-hot-toast';
import { readersApi, collectionsApi } from '../../services/api';
import type { AdminReader, AdminReaderCollection } from '../../types';
import { apiErrorMessage } from '../../utils/apiError';
import { russianDate } from '../../utils/russianDate';
import './ReadersList.css';

/** Тот же потолок, что у сервера (readerListLimit). Нужен, чтобы сказать вслух,
 *  что выдача обрезана: молча обрезанный список администратор принял бы за
 *  полный и решил, что искомого читателя нет вовсе. */
const READER_LIST_CAP = 200;

/**
 * Читатели глазами администратора: путь по жалобе от имени из письма до кнопки
 * снятия подборки.
 *
 * Экран отдельный от «Пользователей» намеренно: там заводят и разжалуют
 * сотрудников, здесь только смотрят. Учётной записью читателя администратор не
 * распоряжается вовсе — ни роли, ни пароля, ни удаления:
 *
 * - сброса пароля читальня не умеет (почты у читателя нет, восстанавливать
 *   нечем), и кнопка обещала бы то, чего нет;
 * - роль читателя не назначается и не снимается через API (validRole знает
 *   только сотруднические роли);
 * - удаление учётной записи не прекращает уже выданный токен — он живёт до 90
 *   суток, — поэтому «удалить читателя» выглядело бы рычагом, которым не
 *   является.
 *
 * Сторожит это решение тест «не предлагает ни сброса пароля, ни смены роли,
 * ни удаления читателя».
 */
export const ReadersList: React.FC = () => {
  const [query, setQuery] = useState('');
  // Отправленная строка поиска. Отдельно от поля ввода: список перезапрашивается
  // по «Найти», а не на каждую букву — читателей ищут по имени из письма целиком.
  const [applied, setApplied] = useState('');
  const [readers, setReaders] = useState<AdminReader[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');

  const [openNickname, setOpenNickname] = useState<string | null>(null);
  const [collections, setCollections] = useState<AdminReaderCollection[] | null>(null);
  const [collectionsError, setCollectionsError] = useState('');
  // Имя, у которого подборки есть, а учётной записи нет: владельца удалили,
  // owner_id обнулился, подпись живёт снимком ника. Ради этого случая ключом и
  // взят ник, и дойти до него надо с экрана, а не голым запросом к API.
  const [orphanNickname, setOrphanNickname] = useState<string | null>(null);

  // Ответы приходят не в том порядке, в каком их спросили. Без этого счётчика
  // два быстрых щелчка подряд кладут подборки первого читателя под раскрытую
  // строку второго — ровно в том разборе, ради которого экран и заведён.
  const collectionsRequest = useRef(0);

  const loadCollections = useCallback(async (nickname: string) => {
    const seq = ++collectionsRequest.current;
    try {
      setCollectionsError('');
      const response = await readersApi.collections(nickname);
      if (seq !== collectionsRequest.current) return;
      setCollections(response.data || []);
    } catch (err: unknown) {
      if (seq !== collectionsRequest.current) return;
      setCollections([]);
      setCollectionsError(apiErrorMessage(err, 'Не удалось загрузить подборки читателя'));
    }
  }, []);

  const loadReaders = useCallback(async () => {
    try {
      setError('');
      const response = await readersApi.list(applied);
      const rows = response.data || [];
      setReaders(rows);
      // Искали конкретное имя, учётной записи с ним нет — но подборки под этим
      // именем могли остаться. Спрашиваем их сразу: иначе осиротевшая подборка
      // недостижима с экрана, хотя весь разбор жалобы затеян ради неё.
      if (rows.length === 0 && applied !== '') {
        setOrphanNickname(applied);
        setOpenNickname(applied);
        await loadCollections(applied);
      } else {
        setOrphanNickname(null);
      }
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось загрузить читателей'));
    } finally {
      setIsLoading(false);
    }
  }, [applied, loadCollections]);

  useEffect(() => {
    // Загрузчик забирает ошибки во внутреннем catch и не реджектится, поэтому
    // промис здесь честно отбрасывается. О правиле set-state-in-effect — тот же
    // разбор, что и в UsersList: переезд на react-query вынесен отдельно.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadReaders();
  }, [loadReaders]);

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    setOpenNickname(null);
    setCollections(null);
    setApplied(query.trim());
  };

  const handleToggle = async (nickname: string) => {
    if (openNickname === nickname) {
      setOpenNickname(null);
      return;
    }
    setOpenNickname(nickname);
    setCollections(null);
    await loadCollections(nickname);
  };

  const handleTakedown = async (collection: AdminReaderCollection, nickname: string) => {
    if (!window.confirm(`Снять подборку «${collection.title}» читателя ${nickname}?`)) return;

    try {
      await collectionsApi.remove(collection.slug, nickname);
      toast.success('Подборка снята');
      await loadCollections(nickname);
      await loadReaders();
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось снять подборку'));
    }
  };

  /** Состояние подборки словами. Снятая с публикации и черновик — разные вещи:
   *  снятую читатели видели, черновика не видел никто, кроме автора, и жалоба
   *  бывает только о первом. */
  const stateLabel = (collection: AdminReaderCollection): string => {
    if (collection.published_at) return 'опубликована';
    return collection.was_published ? 'снята с публикации' : 'черновик';
  };

  const renderCollections = (nickname: string) => (
    <>
      {collectionsError && <div className="error-message">{collectionsError}</div>}
      {collections === null ? (
        <span className="readers-collections-loading">Загрузка подборок…</span>
      ) : collections.length === 0 ? (
        <span className="readers-collections-empty">Подборок нет</span>
      ) : (
        <ul className="readers-collections">
          {collections.map((collection) => (
            <li key={collection.id}>
              {/* Ссылка только у опубликованной: черновик и снятую с публикации
                  сервер отдаёт лишь владельцу (404 и 410 соответственно), и
                  ссылка вела бы в отказ. Снять их при этом можно — снятие
                  правом владельца не меряется. */}
              {collection.published_at ? (
                <Link to={`/collections/${encodeURIComponent(nickname)}/${collection.slug}`}>
                  {collection.title}
                </Link>
              ) : (
                <span className="readers-collection-title">{collection.title}</span>
              )}
              <span className="readers-collection-state">{stateLabel(collection)}</span>
              <button
                type="button"
                className="danger-button"
                onClick={() => void handleTakedown(collection, nickname)}
              >
                Снять
              </button>
            </li>
          ))}
        </ul>
      )}
    </>
  );

  if (isLoading) {
    return <div className="loading-state">Загрузка читателей…</div>;
  }

  return (
    <div className="readers-list-container">
      <h1>Читатели</h1>
      <p className="readers-list-hint">
        Разбор жалобы: найдите читателя по имени из письма, раскройте его подборки и снимите ту, о
        которой речь. Учётная запись при этом остаётся — снимается подборка, а не человек.
      </p>

      {error && <div className="error-message">{error}</div>}

      <form className="readers-search-form" onSubmit={handleSearch} role="search">
        <input
          type="search"
          aria-label="Ник читателя"
          placeholder="Имя читателя"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <button type="submit">Найти</button>
      </form>

      {readers.length === 0 ? (
        orphanNickname ? (
          <div className="readers-orphan">
            <p>
              Учётной записи с именем «{orphanNickname}» нет — возможно, читатель удалён. Подборки,
              подписанные этим именем, остались:
            </p>
            {renderCollections(orphanNickname)}
          </div>
        ) : (
          <p className="readers-list-empty">
            {applied ? `Читателя «${applied}» не нашлось` : 'Читателей пока нет'}
          </p>
        )
      ) : (
        <>
          {readers.length >= READER_LIST_CAP && (
            <p className="readers-list-capped">
              Показаны первые {READER_LIST_CAP} — уточните имя, чтобы увидеть остальных.
            </p>
          )}
          <table className="readers-table">
            <thead>
              <tr>
                <th>Имя</th>
                <th>Заведён</th>
                <th>Подборок</th>
              </tr>
            </thead>
            <tbody>
              {readers.map((reader) => (
                <Fragment key={reader.id}>
                  <tr>
                    <td>
                      <button
                        type="button"
                        className="readers-expand"
                        aria-expanded={openNickname === reader.nickname}
                        onClick={() => void handleToggle(reader.nickname)}
                      >
                        {reader.nickname}
                      </button>
                    </td>
                    <td>{russianDate(reader.created_at)}</td>
                    <td>{reader.collections_count}</td>
                  </tr>
                  {openNickname === reader.nickname && (
                    <tr className="readers-collections-row">
                      <td colSpan={3}>{renderCollections(reader.nickname)}</td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        </>
      )}
    </div>
  );
};
