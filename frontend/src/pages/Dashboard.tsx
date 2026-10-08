import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { shelfApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { forgetRead, readRecent, type LastRead } from '../hooks/useReadingProgress';
import { EditionSummary } from '../components/EditionSummary';
import { VolumeShelf } from '../components/VolumeShelf';
import type { ShelfEdition, ShelfWork, VolumeSummary } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { splitEditionVolumes } from '../utils/editionStats';
import { corpusSentence } from '../utils/corpusSentence';
import { chapterPath, editionPath, workPath } from '../utils/paths';

import { useSite } from '../services/site';
import './Dashboard.css';

/** Собрание с уже отсеянными предисловиями — в том виде, в каком его рисуют. */
interface Shelf {
  edition: ShelfEdition['edition'];
  volumes: VolumeSummary[];
}

/**
 * Порядок полок: сперва те, которых в читальне больше. Прежде собрания шли по
 * алфавиту заголовка — то есть по фамилии автора, — и порядок читался
 * случайным: сорок пять ленинских томов стояли выше четырёх плехановских не
 * почему-либо, а потому что «В» раньше «Г».
 */
function byHoldings(a: Shelf, b: Shelf): number {
  if (a.volumes.length !== b.volumes.length) return b.volumes.length - a.volumes.length;
  const pages = (s: Shelf) => s.volumes.reduce((sum, v) => sum + v.pages_total, 0);
  return pages(b) - pages(a);
}

export const Dashboard: React.FC = () => {
  const [shelves, setShelves] = useState<ShelfEdition[]>([]);
  const [looseWorks, setLooseWorks] = useState<ShelfWork[]>([]);
  const [recent, setRecent] = useState<LastRead[]>(() => readRecent());
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');
  const { user } = useAuth();
  const canEdit = user?.role === 'administrator' || user?.role === 'editor';

  const { siteName } = useSite();
  useDocumentTitle(siteName);

  useEffect(() => {
    const load = async () => {
      try {
        // Один запрос на всю страницу. Прежде их было шесть в две волны:
        // список собраний и каталог работ, а затем — только дождавшись
        // списка — по запросу на каждое собрание за томами. Вторая волна
        // стоила по 350 мс на собрание, потому что тяжёлая часть запроса
        // сводок считается по всему корпусу и от фильтра не зависит; теперь
        // тот же счёт делается на сервере один раз на все собрания.
        //
        // Работу, созданную через интерфейс, приписать к собранию нечем: ни в
        // форме, ни в запросе создания нет такого поля. Без loose_works она не
        // попадала бы никуда и открывалась бы только по набранному руками
        // адресу.
        const { data } = await shelfApi.get();
        setShelves(data.editions);
        setLooseWorks(data.loose_works);
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Не удалось загрузить собрание'));
      } finally {
        setIsLoading(false);
      }
    };

    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    void load();
  }, []);

  // Предваряющие работы (предисловие ко всему собранию или к его части) не
  // несут номера тома и не должны попасть ни в счётчик, ни в штабель —
  // иначе у собрания Маркса и Энгельса счётчик показал бы 52 тома вместо 50, а
  // в штабеле два предисловия легли бы книгами без номера.
  const ordered = useMemo(
    () =>
      shelves
        .map(({ edition, volumes }) => ({
          edition,
          volumes: splitEditionVolumes(volumes).shelf,
        }))
        .sort(byHoldings),
    [shelves],
  );

  if (isLoading) {
    return <div className="loading-state">Загружаем собрание…</div>;
  }

  return (
    <div className="shelf-page">
      {/* Заголовок страницы скрыт: то же самое написано в шапке сайта, и
          видимый повтор занял бы первый экран ничем. Обходу по заголовкам он
          при этом нужен — без него страница начиналась бы сразу с h2. */}
      <h1 className="visually-hidden">{siteName}</h1>

      {error && <div className="error-message">{error}</div>}

      {/* Верх страницы — одно место с двумя жильцами. Пришедшему впервые оно
          называет размер читальни; вернувшемуся к чтению — возвращает туда,
          где он остановился. Второму размер корпуса уже известен, а первому
          некуда продолжать. */}
      {recent.length > 0 ? (
        // Читатель ведёт несколько книг разом, поэтому мест здесь столько,
        // сколько начато, а не одно последнее. Первое — свежее и главное, у
        // него кнопка в полный рост.
        <section className="continue-reading" aria-label="Продолжить чтение">
          <ul className="continue-reading-list">
            {recent.map((entry, i) => {
              const title = entry.chapterTitle || entry.workTitle;
              return (
                <li key={`${entry.workId}:${entry.chapterId}`} className="continue-reading-item">
                  <div className="continue-reading-text">
                    <strong>{title}</strong>
                    <span>
                      {entry.workTitle}
                      {entry.pageNumber !== null && `, стр. ${entry.pageNumber}`}
                    </span>
                  </div>
                  <Link
                    // Строка списка не хранит слаги работы и главы (LastRead в
                    // useReadingProgress.ts) — числовой сегмент здесь
                    // законен, а не запасной вариант: слага для него в
                    // хранилище просто нет.
                    to={chapterPath({ id: entry.workId }, { id: entry.chapterId })}
                    className={i === 0 ? 'btn btn-primary' : 'btn btn-secondary btn-sm'}
                    // Пять одинаковых «Продолжить» подряд читалка экрана не
                    // различила бы — имя несёт главу.
                    aria-label={`Продолжить: ${title}`}
                  >
                    Продолжить
                  </Link>
                  {/* Дочитанное список не убирает сам — по прокрутке «дочитал»
                      не определить. Лишнее читатель снимает крестиком. */}
                  <button
                    type="button"
                    className="continue-reading-forget"
                    aria-label={`Убрать «${title}» из списка`}
                    title="Убрать из списка"
                    onClick={() => {
                      forgetRead(entry.workId, entry.chapterId);
                      setRecent(readRecent());
                    }}
                  >
                    ×
                  </button>
                </li>
              );
            })}
          </ul>
        </section>
      ) : (
        // У пустой читальни размера нет, и фраза про него была бы про ноль.
        ordered.length > 0 && <p className="shelf-intro">{corpusSentence(ordered)}</p>
      )}

      {/* Ссылка на витрину разборов — читательское эссе поверх корпуса.
          Указатель и подборки сюда не дублируются: их и так показывает шапка
          на каждой странице, включая эту. */}
      <nav className="shelf-quicklinks" aria-label="Другие разделы читальни">
        <Link to="/documents">Разборы</Link>
      </nav>

      {ordered.map(({ edition, volumes }) => (
        <section className="shelf-section" key={edition.id}>
          <EditionSummary
            edition={edition}
            volumes={volumes}
            titleHref={editionPath(edition)}
            level="h2"
          />

          {volumes.length === 0 ? (
            // Собрание без томов не пропускается молча: администратор должен
            // видеть, что оно заведено.
            <div className="empty-state">
              <p>Пока ни одного тома.</p>
              {canEdit && (
                <Link to="/works/new" className="btn btn-primary">
                  Загрузить первый
                </Link>
              )}
            </div>
          ) : (
            <VolumeShelf
              volumes={volumes}
              currentWorkId={recent[0]?.workId}
              volumesPlanned={edition.volumes_planned}
              // Собраний на странице пять, и два из них — сорок пять и
              // пятьдесят томов: целиком они дают по полторы тысячи пикселей
              // каждое, и нижние собрания оказываются за краем терпения.
              // Страница собрания (EditionDetail) об этом не просит: за
              // списком томов туда и приходят.
              collapse
            />
          )}
        </section>
      ))}

      {/* Корешок требует агрегатов, которых /works не отдаёт: подписи,
          состава и объёма тома. Нарисовать их нулями значило бы соврать про
          содержимое работы. Поэтому просто список. */}
      {looseWorks.length > 0 && (
        <section className="shelf-section" key="loose">
          <h2 className="shelf-loose-heading">Вне собраний</h2>
          <ul className="loose-works">
            {looseWorks.map((work) => (
              <li key={work.id}>
                <Link to={workPath(work)}>{work.title}</Link>
              </li>
            ))}
          </ul>
        </section>
      )}

      {/* Приглашение загрузить не показывается поверх ошибки: шкаф пуст не
          потому, что в ней ничего нет, а потому, что её не удалось прочитать.
          Ссылки здесь нет: строка действий ниже не зависит от числа собраний
          и её «Добавить том» ведёт туда же — вести на /works/new дважды
          незачем. */}
      {shelves.length === 0 && !error && (
        <div className="empty-state">
          <p>Пока ни одного собрания.</p>
        </div>
      )}

      {/* Не зависит от shelves.length: пустая установка — единственное место,
          откуда можно завести первое собрание, если убрать эту строку, его
          негде будет создать вовсе. Как и приглашение выше, не рисуется
          поверх ошибки — она не значит, что собраний действительно нет. */}
      {canEdit && !error && (
        <div className="shelf-actions">
          <Link to="/works/new" className="btn btn-primary">
            Добавить том
          </Link>
          <Link to="/editions/new" className="btn btn-secondary">
            Создать собрание
          </Link>
        </div>
      )}
    </div>
  );
};
