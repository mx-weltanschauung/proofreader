import { lazy, Suspense } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'react-hot-toast';
import { Dashboard } from './pages/Dashboard';
import { RequireRole } from './components/ProtectedRoute';
import { Layout } from './components/Layout';
import { PageViewBeacon } from './components/PageViewBeacon';
import { ReadingPreferencesProvider } from './contexts/ReadingPreferencesContext';
import { useAuth } from './hooks/useAuth';
import { useToasterColors } from './hooks/useToasterColors';

// Главная грузится вместе с приложением: с неё читальня начинается, и
// отдельным куском она стоила бы лишнего похода на сервер ровно там, где
// страница должна появиться быстрее всего.
//
// Всё остальное — по требованию. В одном куске лежали редактор разметки
// (@uiw/react-md-editor), KaTeX, react-markdown и dnd-kit; главная не
// показывает ни одного из них, а платила за все: 618 КБ в gzip и 0.8 секунды
// до первого запроса к API.
const Login = lazy(() => import('./pages/Login').then((m) => ({ default: m.Login })));
const Join = lazy(() => import('./pages/Join').then((m) => ({ default: m.Join })));
const WorkForm = lazy(() => import('./pages/WorkForm').then((m) => ({ default: m.WorkForm })));
const WorkEdit = lazy(() => import('./pages/WorkEdit').then((m) => ({ default: m.WorkEdit })));
const WorkDetail = lazy(() =>
  import('./pages/WorkDetail').then((m) => ({ default: m.WorkDetail })),
);
const PageEditor = lazy(() =>
  import('./pages/PageEditor').then((m) => ({ default: m.PageEditor })),
);
const PageView = lazy(() => import('./pages/PageView').then((m) => ({ default: m.PageView })));
const PageSuggest = lazy(() =>
  import('./pages/PageSuggest').then((m) => ({ default: m.PageSuggest })),
);
const MySuggestions = lazy(() =>
  import('./pages/MySuggestions').then((m) => ({ default: m.MySuggestions })),
);
const SuggestionQueue = lazy(() =>
  import('./pages/SuggestionQueue').then((m) => ({ default: m.SuggestionQueue })),
);
const ChapterForm = lazy(() =>
  import('./pages/ChapterForm').then((m) => ({ default: m.ChapterForm })),
);
const ChapterView = lazy(() =>
  import('./pages/ChapterView').then((m) => ({ default: m.ChapterView })),
);
const WorkRead = lazy(() => import('./pages/WorkRead').then((m) => ({ default: m.WorkRead })));
const DocumentList = lazy(() =>
  import('./pages/DocumentList').then((m) => ({ default: m.DocumentList })),
);
const DocumentForm = lazy(() =>
  import('./pages/DocumentForm').then((m) => ({ default: m.DocumentForm })),
);
const DocumentView = lazy(() =>
  import('./pages/DocumentView').then((m) => ({ default: m.DocumentView })),
);
const DocumentReviewQueue = lazy(() =>
  import('./pages/DocumentReviewQueue').then((m) => ({ default: m.DocumentReviewQueue })),
);
const EditionForm = lazy(() =>
  import('./pages/EditionForm').then((m) => ({ default: m.EditionForm })),
);
const EditionHighlightsEdit = lazy(() =>
  import('./pages/EditionHighlightsEdit').then((m) => ({ default: m.EditionHighlightsEdit })),
);
const EditionDetail = lazy(() =>
  import('./pages/EditionDetail').then((m) => ({ default: m.EditionDetail })),
);
const Search = lazy(() => import('./pages/Search').then((m) => ({ default: m.Search })));
const ConceptList = lazy(() =>
  import('./pages/ConceptList').then((m) => ({ default: m.ConceptList })),
);
const ConceptView = lazy(() =>
  import('./pages/ConceptView').then((m) => ({ default: m.ConceptView })),
);
const CollectionList = lazy(() =>
  import('./pages/CollectionList').then((m) => ({ default: m.CollectionList })),
);
const CollectionForm = lazy(() =>
  import('./pages/CollectionForm').then((m) => ({ default: m.CollectionForm })),
);
const CollectionView = lazy(() =>
  import('./pages/CollectionView').then((m) => ({ default: m.CollectionView })),
);
const CollectionRead = lazy(() =>
  import('./pages/CollectionRead').then((m) => ({ default: m.CollectionRead })),
);
const Help = lazy(() => import('./pages/Help').then((m) => ({ default: m.Help })));
const Legal = lazy(() => import('./pages/Legal').then((m) => ({ default: m.Legal })));
const Feedback = lazy(() => import('./pages/Feedback').then((m) => ({ default: m.Feedback })));
const UsersList = lazy(() =>
  import('./pages/admin/UsersList').then((m) => ({ default: m.UsersList })),
);
const FeedbackList = lazy(() =>
  import('./pages/admin/FeedbackList').then((m) => ({ default: m.FeedbackList })),
);
const ReadersList = lazy(() =>
  import('./pages/admin/ReadersList').then((m) => ({ default: m.ReadersList })),
);
const StatsAdmin = lazy(() =>
  import('./pages/admin/StatsAdmin').then((m) => ({ default: m.StatsAdmin })),
);
const CacheAdmin = lazy(() =>
  import('./pages/admin/CacheAdmin').then((m) => ({ default: m.CacheAdmin })),
);
const AudioAdmin = lazy(() =>
  import('./pages/admin/AudioAdmin').then((m) => ({ default: m.AudioAdmin })),
);

const queryClient = new QueryClient();

function ThemedToaster() {
  const colors = useToasterColors();

  return (
    <Toaster
      position="top-center"
      toastOptions={{
        duration: 3000,
        style: {
          background: colors.cardBg,
          color: colors.textPrimary,
          border: `1px solid ${colors.cardBorder}`,
          boxShadow: `0 4px 12px ${colors.shadow}`,
        },
        success: {
          duration: 3000,
          iconTheme: {
            primary: colors.success,
            secondary: colors.cardBg,
          },
        },
        error: {
          duration: 4000,
          iconTheme: {
            primary: colors.error,
            secondary: colors.cardBg,
          },
        },
      }}
    />
  );
}

/**
 * Заглушка на время, пока едет кусок страницы. Показывается только при
 * переходе на страницу, чей код ещё не загружен, — то есть один раз на
 * страницу за сеанс, и обычно на доли секунды.
 */
const routeFallback = <div className="loading-state">Загружаем…</div>;

function App() {
  const { isLoading } = useAuth();

  // Состояние восстанавливается автоматически через onRehydrateStorage
  // Токен и user загружаются из localStorage при инициализации

  if (isLoading) {
    return (
      <ReadingPreferencesProvider>
        <div
          style={{
            display: 'flex',
            justifyContent: 'center',
            alignItems: 'center',
            height: '100vh',
            fontSize: '18px',
          }}
        >
          Загрузка…
        </div>
      </ReadingPreferencesProvider>
    );
  }

  return (
    <ReadingPreferencesProvider>
      <ThemedToaster />
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <PageViewBeacon />
          <Layout>
            <Suspense fallback={routeFallback}>
              <Routes>
                <Route path="/login" element={<Login />} />
                <Route path="/join" element={<Join />} />
                <Route path="/" element={<Dashboard />} />
                <Route path="/help" element={<Help />} />
                <Route path="/legal" element={<Legal />} />
                <Route path="/feedback" element={<Feedback />} />
                <Route path="/search" element={<Search />} />
                <Route
                  path="/works/new"
                  element={
                    <RequireRole roles={['administrator', 'editor']}>
                      <WorkForm />
                    </RequireRole>
                  }
                />
                <Route
                  path="/works/:id/edit"
                  element={
                    <RequireRole roles={['administrator', 'editor']}>
                      <WorkEdit />
                    </RequireRole>
                  }
                />
                <Route path="/works/:id" element={<WorkDetail />} />
                <Route path="/works/:workId/pages/:pageNumber" element={<PageView />} />
                <Route path="/works/:workId/pages/:pageNumber/suggest" element={<PageSuggest />} />
                {/* Публичный, без RequireRole: невошедший читатель видит на
                    экране приглашение записаться вместо списка правок. */}
                <Route path="/mine" element={<MySuggestions />} />
                {/* Старый адрес — редирект, не удаление: мог остаться в закладках. */}
                <Route path="/suggestions/mine" element={<Navigate to="/mine" replace />} />
                <Route
                  path="/suggestions/queue"
                  element={
                    <RequireRole roles={['administrator', 'editor']}>
                      <SuggestionQueue />
                    </RequireRole>
                  }
                />
                <Route
                  path="/works/:workId/pages/:pageNumber/edit"
                  element={
                    <RequireRole roles={['administrator', 'editor']}>
                      <PageEditor />
                    </RequireRole>
                  }
                />
                <Route
                  path="/works/:workId/chapters/new"
                  element={
                    <RequireRole roles={['administrator', 'editor']}>
                      <ChapterForm />
                    </RequireRole>
                  }
                />
                <Route
                  path="/works/:workId/chapters/:chapterId/edit"
                  element={
                    <RequireRole roles={['administrator', 'editor']}>
                      <ChapterForm />
                    </RequireRole>
                  }
                />
                <Route path="/works/:workId/read/:pageNumber" element={<WorkRead />} />
                <Route path="/works/:workId/chapters/:chapterId" element={<ChapterView />} />
                <Route path="/documents" element={<DocumentList />} />
                {/* Создание и правка своего разбора доступны любому вошедшему,
                    не только персоналу — mayEditDocument на сервере решает по
                    нику, а не по роли; RequireRole тут не подходит, форма сама
                    показывает приглашение записаться гостю (см.
                    DocumentForm.tsx, тот же приём, что у CollectionForm). */}
                <Route path="/documents/new" element={<DocumentForm />} />
                {/* Длинный (читательский) адрес несёт ник впереди слага — см.
                    utils/documentPaths.ts. Короткий (сотруднический) остаётся
                    без ника, тем же выбором, что у подборок в App.tsx выше. */}
                <Route path="/documents/:nickname/:slug/edit" element={<DocumentForm />} />
                <Route path="/documents/:slug/edit" element={<DocumentForm />} />
                {/* Не ловушка порядка, как у router.go на сервере (gorilla/mux
                    подроутеры честно порядковые): React Router v6 ранжирует
                    маршруты по специфичности сам (computeScore/
                    rankRouteBranches, @remix-run/router) — статический
                    сегмент «review» весит больше динамического «:slug»
                    независимо от того, какая строка ниже. Порядок здесь для
                    параллели с сервером и читаемости, не потому что он
                    решает; перестановка строк была проверена вручную и не
                    ломает адрес. Держится тестом App.test.tsx. */}
                <Route
                  path="/documents/review"
                  element={
                    <RequireRole roles={['administrator', 'editor']}>
                      <DocumentReviewQueue />
                    </RequireRole>
                  }
                />
                <Route path="/documents/:nickname/:slug" element={<DocumentView />} />
                <Route path="/documents/:slug" element={<DocumentView />} />
                <Route
                  path="/editions/new"
                  element={
                    <RequireRole roles={['administrator', 'editor']}>
                      <EditionForm />
                    </RequireRole>
                  }
                />
                <Route
                  path="/editions/:id/edit"
                  element={
                    <RequireRole roles={['administrator', 'editor']}>
                      <EditionForm />
                    </RequireRole>
                  }
                />
                <Route
                  path="/editions/:id/highlights"
                  element={
                    <RequireRole roles={['administrator', 'editor']}>
                      <EditionHighlightsEdit />
                    </RequireRole>
                  }
                />
                <Route path="/editions/:id" element={<EditionDetail />} />
                <Route path="/concepts" element={<ConceptList />} />
                <Route path="/concepts/:slug" element={<ConceptView />} />
                <Route path="/collections" element={<CollectionList />} />
                {/* Создание и правка своей подборки доступны любому вошедшему,
                    не только персоналу (POST /collections принимает читателя
                    наравне с редактором/администратором) — RequireRole тут не
                    подходит, форма сама показывает приглашение записаться
                    гостю (см. CollectionForm.tsx). */}
                <Route path="/collections/new" element={<CollectionForm />} />
                <Route path="/collections/:slug/edit" element={<CollectionForm />} />
                {/* Длинный (читательский) адрес несёт ник впереди слага — см.
                    paths.ts. Короткий адрес ниже остаётся витринным. */}
                <Route
                  path="/collections/:nickname/:slug/read/:itemId"
                  element={<CollectionRead />}
                />
                <Route path="/collections/:nickname/:slug" element={<CollectionView />} />
                <Route path="/collections/:slug/read/:itemId" element={<CollectionRead />} />
                <Route path="/collections/:slug" element={<CollectionView />} />
                <Route
                  path="/admin/users"
                  element={
                    <RequireRole roles={['administrator']}>
                      <UsersList />
                    </RequireRole>
                  }
                />
                <Route
                  path="/admin/readers"
                  element={
                    <RequireRole roles={['administrator']}>
                      <ReadersList />
                    </RequireRole>
                  }
                />
                <Route
                  path="/admin/feedback"
                  element={
                    <RequireRole roles={['administrator']}>
                      <FeedbackList />
                    </RequireRole>
                  }
                />
                <Route
                  path="/admin/cache"
                  element={
                    <RequireRole roles={['administrator']}>
                      <CacheAdmin />
                    </RequireRole>
                  }
                />
                <Route
                  path="/admin/audio"
                  element={
                    <RequireRole roles={['administrator', 'editor']}>
                      <AudioAdmin />
                    </RequireRole>
                  }
                />
                <Route
                  path="/admin/stats"
                  element={
                    <RequireRole roles={['administrator']}>
                      <StatsAdmin />
                    </RequireRole>
                  }
                />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
            </Suspense>
          </Layout>
        </BrowserRouter>
      </QueryClientProvider>
    </ReadingPreferencesProvider>
  );
}

export default App;
