package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
)

// access — уровень доступа маршрута. Таблица ниже перечисляет ВСЕ маршруты
// API: новый маршрут, не попавший в неё, роняет тест. Это и есть смысл
// сторожа — заставить классифицировать доступ осознанно, а не унаследовать
// его от соседней строки.
type access int

const (
	accessPublic access = iota
	// accessAny — валидный токен любой роли, RequireRole не стоит вовсе.
	// Это не недосмотр, но и не образец для копирования: на подроутере
	// authRequired (router.go) правило "каждый непубличный маршрут под
	// RequireRole" ломается по построению только для /auth/me — продлить и
	// посмотреть СОБСТВЕННУЮ сессию вправе всякий вошедший, потому что это
	// действие над своей же личностью, а не доступ к чужим данным.
	//
	// До читательских токенов "любая роль" и так означало "сотрудник",
	// потому что учётку заводил только администратор; это было случайным
	// совпадением, а не причиной, по которой /auth/me можно было не закрывать
	// RequireRole. Другому маршруту на authRequired то же обоснование не
	// переносится автоматически: история правок полосы (page_versions) ушла
	// на accessStaff именно потому, что там доступ идёт к чужим данным
	// редакционного процесса, а не к своим собственным — см. её строку ниже.
	accessAny
	accessReader
	accessStaff
	accessAdmin
)

// routeAccess: ключ — "МЕТОД путь" ровно так, как его печатает mux.Walk.
//
// Уровень проставлен по тому, на каком подроутере строка объявлена в
// router.go (public/authRequired/staff/reader/admin), а не по догадке о
// смысле маршрута.
var routeAccess = map[string]access{
	// Вне /api: страницы для краулеров и служебные адреса читальни. Без
	// единого middleware — см. router.go, начало Setup().
	"GET /seo":          accessPublic,
	"HEAD /seo":         accessPublic,
	"GET /robots.txt":   accessPublic,
	"HEAD /robots.txt":  accessPublic,
	"GET /sitemap.xml":  accessPublic,
	"HEAD /sitemap.xml": accessPublic,
	"GET /sitemap-":     accessPublic,
	"HEAD /sitemap-":    accessPublic,
	"GET /og/":          accessPublic,
	"HEAD /og/":         accessPublic,

	// MCP-сервер читальни: публичный, без токена (спека corpus-mcp, «Доступ»).
	"GET /mcp":    accessPublic,
	"HEAD /mcp":   accessPublic,
	"POST /mcp":   accessPublic,
	"DELETE /mcp": accessPublic,

	// Каталог OPDS: публичный, без токена (спека opds-catalog).
	"GET /opds":  accessPublic,
	"HEAD /opds": accessPublic,

	// api.HandleFunc напрямую, до появления подроутеров — публично.
	"GET /api/health":  accessPublic,
	"GET /api/version": accessPublic,

	// authRoutes — обе двери открыты намеренно, ни одного middleware.
	"POST /api/auth/login":  accessPublic,
	"POST /api/auth/reader": accessPublic,

	// authRequired — валидный токен любой роли, см. accessAny выше.
	"GET /api/auth/me": accessAny,
	// Уход читателя объявлен на подроутере reader (пускает и staff на
	// уровне middleware — см. её комментарий в router.go), но AuthHandler.
	// DeleteMe отказывает сотруднику внутри себя честным 403. Классификация
	// идёт по подроутеру, как и у всех остальных строк этой таблицы;
	// TestDeleteMeRefusesStaff (auth_handler_test.go) проверяет то, что этот
	// уровень не видит, — сам код отказа.
	"DELETE /api/auth/me": accessReader,

	// public
	"GET /api/categories":                           accessPublic,
	"GET /api/categories/{id}":                      accessPublic,
	"GET /api/editions":                             accessPublic,
	"GET /api/editions/{id}":                        accessPublic,
	"GET /api/editions/{id}/works":                  accessPublic,
	"GET /api/editions/{id}/highlights":             accessPublic,
	"GET /api/journals":                             accessPublic,
	"GET /api/journals/{slug}":                      accessPublic,
	"POST /api/journals":                            accessStaff,
	"PUT /api/journals/{id}":                        accessStaff,
	"POST /api/journals/{id}/issues":                accessStaff,
	"PUT /api/journal-issues/{id}":                  accessStaff,
	"GET /api/persons":                              accessPublic,
	"GET /api/persons/{slug}":                       accessPublic,
	"POST /api/persons":                             accessStaff,
	"PUT /api/persons/{id}":                         accessStaff,
	"POST /api/persons/{id}/merge":                  accessStaff,
	"PUT /api/works/{workId}/chapters/{id}/credits": accessStaff,
	"GET /api/site":                                 accessPublic,
	"GET /api/shelf":                                accessPublic,
	"GET /api/static-archive":                       accessPublic,
	"GET /api/static-archive/download":              accessPublic,
	// Маячок посещаемости: без токена, отвечает 204 на всё.
	"POST /api/hit":                                        accessPublic,
	"GET /api/search":                                      accessPublic,
	"GET /api/search/pages":                                accessPublic,
	"GET /api/works":                                       accessPublic,
	"GET /api/works/{id}":                                  accessPublic,
	"GET /api/works/{id}/notes":                            accessPublic,
	"GET /api/works/{id}/download":                         accessPublic,
	"GET /api/works/{workId}/pages":                        accessPublic,
	"GET /api/works/{workId}/pages/{pageId}":               accessPublic,
	"GET /api/works/{workId}/pages/{pageId}/render":        accessPublic,
	"GET /api/works/{workId}/pages/{pageId}/blocks":        accessPublic,
	"GET /api/works/{workId}/pages/by-number/{pageNumber}": accessPublic,
	"GET /api/works/{workId}/page-map":                     accessPublic,
	"POST /api/works/{workId}/pages/{pageId}/suggestions":  accessReader,
	"GET /api/suggestions/mine":                            accessReader,
	// Подборка: владение читателем, черновик и публикация. Правку и снятие с
	// публикации может позвать только владелец (или сотрудник — легаси-строки
	// без владельца), см. mayEdit в collection_handler.go.
	"POST /api/collections":                             accessReader,
	"PUT /api/collections/{slug}":                       accessReader,
	"DELETE /api/collections/{slug}":                    accessReader,
	"POST /api/collections/{slug}/items":                accessReader,
	"PUT /api/collections/{slug}/items/{itemId}":        accessReader,
	"DELETE /api/collections/{slug}/items/{itemId}":     accessReader,
	"PATCH /api/collections/{slug}/items/{itemId}/move": accessReader,
	"POST /api/collections/{slug}/publish":              accessReader,
	"POST /api/collections/{slug}/unpublish":            accessReader,
	// Второй, более длинный вид адреса — /collections/{ник}/{слаг}, для
	// читательских подборок. Те же обработчики и та же роль, что у короткого
	// адреса выше — двойник существует только на уровне маршрута.
	"PUT /api/collections/{nickname}/{slug}":                       accessReader,
	"DELETE /api/collections/{nickname}/{slug}":                    accessReader,
	"POST /api/collections/{nickname}/{slug}/items":                accessReader,
	"PUT /api/collections/{nickname}/{slug}/items/{itemId}":        accessReader,
	"DELETE /api/collections/{nickname}/{slug}/items/{itemId}":     accessReader,
	"PATCH /api/collections/{nickname}/{slug}/items/{itemId}/move": accessReader,
	"POST /api/collections/{nickname}/{slug}/publish":              accessReader,
	"POST /api/collections/{nickname}/{slug}/unpublish":            accessReader,
	"GET /api/concepts":                                          accessPublic,
	"GET /api/concepts/{slug}":                                   accessPublic,
	"GET /api/concepts/{slug}/fragments":                         accessPublic,
	"GET /api/concepts/{slug}/references/{refId}/pages/{pageId}": accessPublic,
	"GET /api/works/{workId}/pages/{pageId}/concepts":            accessPublic,
	"GET /api/works/{workId}/chapters":                           accessPublic,
	"GET /api/works/{workId}/chapters/{id}":                      accessPublic,
	"GET /api/works/{workId}/chapters/{id}/pages":                accessPublic,
	"GET /api/works/{workId}/chapters/{id}/download":             accessPublic,
	"GET /api/works/{workId}/reading":                            accessPublic,

	// Озвучка: чтение и редиректы публичны, очередь — сотруднику (worker
	// входит учёткой редактора/администратора боевого).
	"GET /api/works/{id}/audio":                                 accessPublic,
	"GET /api/audio/{id:[0-9]+}.opus":                           accessPublic,
	"GET /api/audio/rec/{id:[0-9]+}":                            accessPublic,
	"HEAD /api/audio/{id:[0-9]+}.opus":                          accessPublic,
	"HEAD /api/audio/rec/{id:[0-9]+}":                           accessPublic,
	"POST /api/works/{id}/audio/queue":                          accessStaff,
	"POST /api/works/{id}/audio/requeue-stale":                  accessStaff,
	"POST /api/works/{id}/audio/uploads":                        accessStaff,
	"POST /api/works/{id}/audio/tracks":                         accessStaff,
	"DELETE /api/works/{id}/audio/objects":                      accessStaff,
	"POST /api/works/{workId}/chapters/{id}/recordings/uploads": accessStaff,
	"POST /api/works/{workId}/chapters/{id}/recordings":         accessStaff,
	"PATCH /api/audio/rec/{id:[0-9]+}":                          accessStaff,
	"DELETE /api/audio/rec/{id:[0-9]+}":                         accessStaff,
	"GET /api/audio/queue":                                      accessStaff,
	"POST /api/audio/queue/claim":                               accessStaff,
	"DELETE /api/audio/queue/{id:[0-9]+}":                       accessStaff,
	"POST /api/audio/queue/{id:[0-9]+}/retry":                   accessStaff,
	"POST /api/audio/queue/{id:[0-9]+}/result":                  accessStaff,
	"POST /api/feedback":                                        accessPublic,
	"GET /api/documents":                                        accessPublic,
	"GET /api/documents/{slug}":                                 accessPublic,
	"GET /api/documents/{slug}/view":                            accessPublic,
	"GET /api/documents/{slug}/cuts/{cutId}":                    accessPublic,
	// Второй, более длинный вид адреса — /documents/{ник}/{слаг}, для
	// читательских разборов. Тот же строй, что у подборок выше: двойник
	// существует только на уровне маршрута, обработчик и роль те же.
	"GET /api/documents/{nickname}/{slug}":              accessPublic,
	"GET /api/documents/{nickname}/{slug}/view":         accessPublic,
	"GET /api/documents/{nickname}/{slug}/cuts/{cutId}": accessPublic,
	// Своё у читателя: mayEditDocument внутри обработчика различает по
	// нику-снимку, а не по роли — подроутер reader, не staff.
	"POST /api/documents":                                  accessReader,
	"PUT /api/documents/{slug}":                            accessReader,
	"DELETE /api/documents/{slug}":                         accessReader,
	"POST /api/documents/{slug}/cuts":                      accessReader,
	"DELETE /api/documents/{slug}/cuts/{cutId}":            accessReader,
	"GET /api/documents/mine":                              accessReader,
	"POST /api/documents/{slug}/submit":                    accessReader,
	"POST /api/documents/{slug}/unpublish":                 accessReader,
	"PUT /api/documents/{nickname}/{slug}":                 accessReader,
	"DELETE /api/documents/{nickname}/{slug}":              accessReader,
	"POST /api/documents/{nickname}/{slug}/cuts":           accessReader,
	"DELETE /api/documents/{nickname}/{slug}/cuts/{cutId}": accessReader,
	"POST /api/documents/{nickname}/{slug}/submit":         accessReader,
	"POST /api/documents/{nickname}/{slug}/unpublish":      accessReader,
	"GET /api/documents/review":                            accessStaff,
	"POST /api/documents/{slug}/approve":                   accessStaff,
	"POST /api/documents/{slug}/reject":                    accessStaff,
	"POST /api/documents/{nickname}/{slug}/approve":        accessStaff,
	"POST /api/documents/{nickname}/{slug}/reject":         accessStaff,
	"GET /api/collections":                                 accessPublic,
	// Личные данные читателя (включая черновики) — стоит на reader, как и
	// "GET /api/suggestions/mine" выше по той же причине. Ручная проверка
	// claims внутри CollectionHandler.Mine осталась страховкой на случай
	// переезда маршрута; отдельный тест TestCollectionsMineRejectsAnonymous и
	// TestCollectionsMineRouteNotSwallowedByGenericSlug проверяют оба слоя.
	"GET /api/collections/mine":                                   accessReader,
	"GET /api/collections/{slug}":                                 accessPublic,
	"GET /api/collections/{slug}/items/{itemId}/pages":            accessPublic,
	"GET /api/collections/{slug}/download":                        accessPublic,
	"GET /api/collections/{nickname}/{slug}":                      accessPublic,
	"GET /api/collections/{nickname}/{slug}/items/{itemId}/pages": accessPublic,
	"GET /api/collections/{nickname}/{slug}/download":             accessPublic,

	// staff — editor/administrator
	"POST /api/categories":                           accessStaff,
	"PUT /api/categories/{id}":                       accessStaff,
	"DELETE /api/categories/{id}":                    accessStaff,
	"POST /api/editions":                             accessStaff,
	"PUT /api/editions/{id}":                         accessStaff,
	"DELETE /api/editions/{id}":                      accessStaff,
	"PUT /api/editions/{id}/highlights":              accessStaff,
	"POST /api/works":                                accessStaff,
	"PUT /api/works/{id}":                            accessStaff,
	"DELETE /api/works/{id}":                         accessStaff,
	"POST /api/works/{id}/upload":                    accessStaff,
	"GET /api/works/{id}/apparatus":                  accessStaff,
	"DELETE /api/works/{id}/apparatus":               accessStaff,
	"POST /api/works/{id}/create-pages":              accessStaff,
	"POST /api/works/{id}/categories":                accessStaff,
	"DELETE /api/works/{id}/categories/{categoryId}": accessStaff,
	"PUT /api/works/{workId}/pages/{pageId}":         accessStaff,
	// История правок полосы: полный прежний текст, включая убранное точечной
	// правкой (в том числе по жалобе) — доступ к чужим данным редакционного
	// процесса, не своим собственным. accessAny ей не подходит, см. коммент
	// на accessAny выше.
	"GET /api/works/{workId}/pages/{pageId}/versions":                      accessStaff,
	"GET /api/works/{workId}/pages/{pageId}/versions/{versionId}":          accessStaff,
	"POST /api/works/{workId}/pages/{pageId}/versions/{versionId}/restore": accessStaff,
	"GET /api/suggestions":                             accessStaff,
	"GET /api/suggestions/{id}":                        accessStaff,
	"POST /api/suggestions/{id}/accept":                accessStaff,
	"POST /api/suggestions/{id}/reject":                accessStaff,
	"DELETE /api/suggestions/rejected":                 accessStaff,
	"DELETE /api/suggestions/{id}":                     accessStaff,
	"POST /api/editions/{id}/index/import":             accessStaff,
	"PUT /api/concepts/{slug}/references/{refId}/cuts": accessStaff,
	"POST /api/works/{workId}/chapters":                accessStaff,
	"PUT /api/works/{workId}/chapters/{id}":            accessStaff,
	"DELETE /api/works/{workId}/chapters/{id}":         accessStaff,
	"PATCH /api/works/{workId}/chapters/{id}/move":     accessStaff,
	"DELETE /api/works/{workId}/chapters/{id}/cache":   accessStaff,

	// admin — administrator only
	"GET /api/users":         accessAdmin,
	"POST /api/users":        accessAdmin,
	"PUT /api/users/{id}":    accessAdmin,
	"DELETE /api/users/{id}": accessAdmin,
	// Читатели глазами администратора: список и подборки одного ника. Личные
	// данные, поэтому не staff, а admin — и проверяется роль, а не факт входа.
	"GET /api/readers":                        accessAdmin,
	"GET /api/readers/{nickname}/collections": accessAdmin,
	"GET /api/feedback":                       accessAdmin,
	"GET /api/feedback/unread-count":          accessAdmin,
	"PATCH /api/feedback/{id}":                accessAdmin,
	"DELETE /api/feedback/{id}":               accessAdmin,
	"GET /api/cache/stats":                    accessAdmin,
	"GET /api/admin/stats/traffic":            accessAdmin,
	"GET /api/admin/stats/top":                accessAdmin,
	"GET /api/admin/stats/referrers":          accessAdmin,
	"GET /api/admin/stats/searches":           accessAdmin,
	"GET /api/admin/stats/crawlers":           accessAdmin,
	"GET /api/admin/stats/devices":            accessAdmin,
	"GET /api/admin/stats/health":             accessAdmin,
	"DELETE /api/cache":                       accessAdmin,
	"GET /api/export":                         accessAdmin,
}

func TestRouterClassifiesEveryRoute(t *testing.T) {
	r := newTestRouter(t).Setup()

	var unknown []string
	_ = r.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		tpl, err := route.GetPathTemplate()
		if err != nil {
			return nil
		}
		methods, err := route.GetMethods()
		if err != nil {
			return nil
		}
		for _, m := range methods {
			key := m + " " + tpl
			if _, ok := routeAccess[key]; !ok {
				unknown = append(unknown, key)
			}
		}
		return nil
	})

	if len(unknown) > 0 {
		t.Fatalf("маршруты без объявленного уровня доступа:\n%s",
			strings.Join(unknown, "\n"))
	}
}

// Без токена непубличный маршрут обязан отвечать 401. Проверка
// короткозамкнутая: до обработчика запрос не доходит, поэтому фальшивые
// хранилища не нужны — а там, где всё же дойдёт (регрессия сняла
// RequireRole/AuthMiddleware с настоящего маршрута с нулевым обработчиком),
// serveWithRecover превращает панику в код, который просто не равен 401,
// вместо того чтобы уронить процесс и не проверить остаток таблицы. См. её
// докблок в router_access_test.go.
func TestNonPublicRoutesRejectAnonymous(t *testing.T) {
	r := newTestRouter(t).Setup()

	for key, lvl := range routeAccess {
		if lvl == accessPublic {
			continue
		}
		method, path := splitRouteKey(key)
		req := httptest.NewRequest(method, concreteURL(path), nil)
		code := serveWithRecover(t, r, req)

		if code != http.StatusUnauthorized {
			t.Errorf("%s без токена ответил %d, ждали 401", key, code)
		}
	}
}

// Зеркало TestNonPublicRoutesRejectAnonymous для другой стороны той же
// границы. Тот тест ловит регрессию «непубличный маршрут ослаб до
// анонимного доступа»; этот — обратную и настолько же дорогую: «публичный
// GET уехал под стражника, и анонимный читатель перестал читать». Без
// этого теста такую регрессию не ловит ничего — TestNonPublicRoutesRejectAnonymous
// пропускает всё помеченное accessPublic по построению, а TestAccessMatrix
// проверяет публичное чтение по курированному списку руками (заметно короче
// полной таблицы routeAccess: например, все три вида адреса подборки с
// ником — GET .../{nickname}/{slug}, его .../items/{itemId}/pages и
// .../download — в тот список не входят, хотя лежат ровно в зоне
// наибольшей плотности совпадений по форме пути с читательским
// подроутером).
//
// serveWithRecover нужен по той же причине, что и во всех соседних тестах
// этого файла: паника на нулевом обработчике доказывает, что запрос дошёл
// (прошёл мимо стражника), и читается как код, который не равен 401, — то
// есть ровно то, что здесь и проверяется, без надобности в настоящих
// хранилищах.
func TestPublicRoutesDoNotRejectAnonymous(t *testing.T) {
	r := newTestRouter(t).Setup()

	for key, lvl := range routeAccess {
		if lvl != accessPublic {
			continue
		}
		method, path := splitRouteKey(key)
		req := httptest.NewRequest(method, concreteURL(path), nil)
		code := serveWithRecover(t, r, req)

		if code == http.StatusUnauthorized {
			t.Errorf("%s без токена ответил 401 — публичный маршрут перестал быть публичным", key)
		}
	}
}

// Читательский токен на сотрудническом маршруте обязан получить 403.
// Это та самая проверка «роль, а не факт входа»: тем же секретом подписан
// токен постороннего. serveWithRecover нужен по той же причине, что и выше —
// сторож всей ролевой модели не должен сам уничтожаться той регрессией,
// которую обязан ловить.
func TestStaffRoutesRejectReaderToken(t *testing.T) {
	r := newTestRouter(t).Setup()
	token := readerToken(t)

	for key, lvl := range routeAccess {
		if lvl != accessStaff && lvl != accessAdmin {
			continue
		}
		method, path := splitRouteKey(key)
		req := httptest.NewRequest(method, concreteURL(path), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		code := serveWithRecover(t, r, req)

		if code != http.StatusForbidden {
			t.Errorf("%s с читательским токеном ответил %d, ждали 403", key, code)
		}
	}
}

// TestReaderRoutesAcceptReaderToken — зеркало TestStaffRoutesRejectReaderToken
// для входящей стороны той же границы. Пять прежних сторожей этого файла
// ловят ослабление стражи (маршрут стал доступнее заявленного) и пропускают
// её ужесточение (маршрут остался строже заявленного): анониму
// TestNonPublicRoutesRejectAnonymous по-прежнему доволен 401-м, а
// читательский токен на сотрудническую строку никто не предъявляет —
// TestStaffRoutesRejectReaderToken перебирает только строки, помеченные
// accessStaff/accessAdmin. Для прежнего кода это было терпимо: ужесточение
// значило «строже, чем задумано», а читателей в природе не было. С приходом
// читательских маршрутов (задача 5) ужесточение стало настоящим дефектом —
// строка таблицы, объявленная accessReader, а маршрут которой молча остался
// на подроутере staff, отвечает читателю 401/403 на его же собственном
// разборе, и весь прежний набор при этом останется зелёным. Что вернёт
// обработчик дальше (400 на пустое тело, 404 на несуществующий id) — не
// проверяется: сторож про границу доступа, а не про поведение обработчика.
func TestReaderRoutesAcceptReaderToken(t *testing.T) {
	r := newTestRouter(t).Setup()
	token := readerToken(t)

	for key, lvl := range routeAccess {
		if lvl != accessReader {
			continue
		}
		method, path := splitRouteKey(key)
		req := httptest.NewRequest(method, concreteURL(path), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		code := serveWithRecover(t, r, req)

		if code == http.StatusUnauthorized || code == http.StatusForbidden {
			t.Errorf("%s с читательским токеном ответил %d — граница доступа не пускает читателя, для которого маршрут объявлен", key, code)
		}
	}
}

// TestAdminRoutesRejectEditorToken — зеркало TestStaffRoutesRejectReaderToken
// для ЕЩЁ ОДНОЙ границы, а не той же самой. Тот тест предъявляет читательский
// токен и строкам accessStaff, и строкам accessAdmin — админский маршрут
// выглядит защищённым, потому что читатель отвергается на обеих ролях
// одинаковым 403. Но читатель тут ничего не доказывает про различие
// сотрудник/администратор: он ниже обеих ролей и не проверяет границу МЕЖДУ
// ними. Токеном редактора — ролью ровно на ступень ниже администратора — эту
// границу не предъявляет никто. Уедь `/api/users`, `/api/export` или
// `DELETE /api/cache` молча на подроутер staff вместо admin — таблица
// останется прежней, читателя маршрут по-прежнему отвергнет, весь набор
// останется зелёным, а любой редактор получит управление учётными записями,
// выгрузку корпуса целиком и сброс кэша. Та же форма дыры, что закрыта
// TestReaderRoutesAcceptReaderToken этажом ниже (там — граница читатель/
// сотрудник, здесь — сотрудник/администратор), и с тем же результатом: набор
// ловит ослабление стражи и пропускает её частичное ужесточение вбок, когда
// строгая роль подменяется соседней по числу, а не отсутствующей вовсе.
//
// Что вернёт обработчик дальше — не проверяется, как и у всех соседей этого
// файла: сторож про границу доступа, а не про поведение обработчика.
func TestAdminRoutesRejectEditorToken(t *testing.T) {
	r := newTestRouter(t).Setup()
	token := tokenForRole(t, models.RoleEditor)

	for key, lvl := range routeAccess {
		if lvl != accessAdmin {
			continue
		}
		method, path := splitRouteKey(key)
		req := httptest.NewRequest(method, concreteURL(path), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		code := serveWithRecover(t, r, req)

		if code != http.StatusForbidden {
			t.Errorf("%s с токеном редактора ответил %d, ждали 403", key, code)
		}
	}
}

// TestRequireRoleRejectsRoleOutsideEnum: токен с ролью вне закрытого
// перечня (reader/editor/administrator) обязан получить 403 от самого
// RequireRole — а не пройти мимо него благодаря собственной проверке claims
// внутри обработчика.
//
// Такую роль боевой код выдать не может: GenerateToken принимает только
// значения из перечня, а третье ("proofreader") снято ещё в T2/T3. Получить
// токен вроде этого можно только подделав подпись, зная JWT_SECRET, — то есть
// при полной компрометации сервера. Тест ниже проверяет не достижимое
// состояние системы, а поведение стражника: сравнивает код ответа, а не то,
// кто его выдал.
//
// Зачем это нужно при живых TestNonPublicRoutesRejectAnonymous (тоже 401) и
// TestStaffRoutesRejectReaderToken (тоже 403, но токеном ИЗ перечня): у
// Mine/Accept/Reject/RestoreVersion есть собственная страховка "claims
// есть — значит, дальше" ещё до обработчика (см. их комментарии
// "страховка на случай, если маршрут когда-нибудь переедет"). Она отвечает
// 401 анонимному запросу тем же кодом, что и AuthMiddleware, и слепа именно
// к сценарию этого теста: RequireRole сняли, а claims у запроса ЕСТЬ (роль
// вне перечня, не отсутствие токена) — собственная проверка обработчика
// пропустит их не глядя на значение роли, он упадёт на nil-хранилище, и
// serveWithRecover прочитает панику как 200-ish, а не 403. Единственный
// источник 403 в этой ситуации — сам RequireRole, и только этот тест
// требует именно 403 (не "не 401"), поэтому только он ловит пропажу
// RequireRole на маршруте с такой страховкой.
//
// accessPublic исключён по смыслу — там доступ не завязан на роль. accessAny
// исключён туда же и по той же причине, а не по недосмотру: RequireRole на
// /auth/me не стоит НАМЕРЕННО (см. её комментарий у объявления accessAny
// выше), и токен с любой ролью, включая эту синтетическую, обязан пройти
// дальше, а не получить 403.
func TestRequireRoleRejectsRoleOutsideEnum(t *testing.T) {
	r := newTestRouter(t).Setup()
	token := nonsenseRoleToken(t)

	for key, lvl := range routeAccess {
		if lvl == accessPublic || lvl == accessAny {
			continue
		}
		method, path := splitRouteKey(key)
		req := httptest.NewRequest(method, concreteURL(path), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		code := serveWithRecover(t, r, req)

		if code != http.StatusForbidden {
			t.Errorf("%s с ролью вне перечня ответил %d, ждали 403", key, code)
		}
	}
}

// readerToken выпускает настоящий токен читателя тем же auth.Service, что
// собран для тестового роутера (общий "test-secret" — см. createTestRouter).
func readerToken(t *testing.T) string {
	t.Helper()
	return tokenForRole(t, models.RoleReader)
}

// nonsenseRoleToken выпускает токен с ролью вне закрытого перечня ролей.
// tokenForRole не проверяет значение — принимает любую строку (см. её
// определение в router_access_test.go), поэтому такой токен подписывается
// тем же "test-secret" без затруднений; см. комментарий у
// TestRequireRoleRejectsRoleOutsideEnum о том, почему такой токен не
// описывает достижимое состояние боевой системы.
func nonsenseRoleToken(t *testing.T) string {
	t.Helper()
	return tokenForRole(t, models.UserRole("никто"))
}

// splitRouteKey разбирает ключ таблицы ("МЕТОД путь") обратно на составные
// части — обратная операция тому, что делает TestRouterClassifiesEveryRoute
// при сборке ключа из mux.Walk.
func splitRouteKey(key string) (method, path string) {
	parts := strings.SplitN(key, " ", 2)
	return parts[0], parts[1]
}

// segmentVar — сегмент пути в фигурных скобках: {id}, {workId}, {slug}, ...
var segmentVar = regexp.MustCompile(`\{[^}]+\}`)

// concreteURL подставляет вместо переменного сегмента конкретное значение:
// "1" почти везде (числовой id/pageId/workId/...), "x" под {slug} — единственный
// строковый сегмент в таблице маршрутов, которому число не подходит.
func concreteURL(path string) string {
	return segmentVar.ReplaceAllStringFunc(path, func(seg string) string {
		if strings.Contains(seg, "slug") {
			return "x"
		}
		return "1"
	})
}
