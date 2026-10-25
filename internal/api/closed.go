package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"proofreader/internal/middleware"
	"proofreader/internal/models"
)

// staffContext — несёт ли контекст claims сотрудника (editor, administrator).
// Нулевое значение — не сотрудник: контекст без claims (context.Background(),
// краулер, MCP, статическая сборка) закрытое не видит. isStaffRequest — то же
// по запросу.
func staffContext(ctx context.Context) bool {
	claims, ok := middleware.GetUserFromContext(ctx)
	return ok && claims != nil &&
		(claims.Role == models.RoleEditor || claims.Role == models.RoleAdministrator)
}

// ClosedSet — множество id закрытых работ; nil значит «ничего не закрыто».
// Сотруднику и экземпляру без хранилища отдаётся именно nil: нулевое значение
// ничего не скрывает.
type ClosedSet map[int64]bool

// Has — закрыта ли работа; на nil-множестве всегда false.
func (s ClosedSet) Has(id int64) bool { return s[id] }

// closedSetFor — множество закрытых работ для списков и отборов. Сотрудник
// видит всё, поэтому ему и при store == nil — nil, без похода в базу.
func closedSetFor(r *http.Request, store ClosedStore) (ClosedSet, error) {
	if store == nil || isStaffRequest(r) {
		return nil, nil
	}
	ids, err := store.ClosedIDs(r.Context())
	if err != nil {
		return nil, err
	}
	return ClosedSet(ids), nil
}

// fullClosedSet — полное множество закрытых работ для каналов без токена
// (краулер, статическая копия): сотрудника там нет, поэтому множество
// отдаётся всегда, а не по запросу. nil-хранилище — nil-множество: ничего не
// закрыто (экземпляр без закрытых работ).
func fullClosedSet(ctx context.Context, store ClosedStore) (ClosedSet, error) {
	if store == nil {
		return nil, nil
	}
	ids, err := store.ClosedIDs(ctx)
	if err != nil {
		return nil, err
	}
	return ClosedSet(ids), nil
}

// worksRoutePrefix — начало шаблона маршрута, чей первый сегмент после него —
// идентификатор работы.
const worksRoutePrefix = "/api/works/{"

// workVarOf — имя переменной работы в шаблоне маршрута ("id" или "workId"),
// если шаблон начинается с /api/works/{…}. Шаблон может нести регулярку
// ({workId:[0-9]+}), поэтому имя берётся до ':' или '}', а не сверяется
// строкой с «{id}».
func workVarOf(template string) (string, bool) {
	rest, ok := strings.CutPrefix(template, worksRoutePrefix)
	if !ok {
		return "", false
	}
	end := strings.IndexAny(rest, ":}")
	if end <= 0 {
		return "", false
	}
	name := rest[:end]
	if name != "id" && name != "workId" {
		return "", false
	}
	return name, true
}

// closedGate — промежуточный слой подроутеров public и reader: маршрут тома
// (/api/works/{id…}, /api/works/{workId…}) для закрытой работы отвечает
// не-сотруднику 404, не доходя до обработчика и его кэша. Ставится ПОСЛЕ
// OptionalAuth/AuthMiddleware — иначе isStaffRequest не видит токен. Нечисловой
// id проходит дальше: обработчик ответит на него сам.
func closedGate(store ClosedStore) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if store == nil || isStaffRequest(r) {
				next.ServeHTTP(w, r)
				return
			}
			route := mux.CurrentRoute(r)
			if route == nil {
				next.ServeHTTP(w, r)
				return
			}
			template, err := route.GetPathTemplate()
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			name, ok := workVarOf(template)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			id, err := strconv.ParseInt(mux.Vars(r)[name], 10, 64)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			closedBy, err := store.ClosedBy(r.Context(), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "Ошибка сервера")
				return
			}
			if closedBy != "" {
				writeError(w, http.StatusNotFound, "Не найдено")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
