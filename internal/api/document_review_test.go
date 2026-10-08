package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"proofreader/internal/auth"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// Правка опубликованного создаёт НОВУЮ редакцию, ждущую модерации; на людях
// остаётся прежняя одобренная. Круг целиком, на уровне обработчиков.
func TestApproveSwapsShownEditionOnlyOnApproval(t *testing.T) {
	store := newFakeDocumentStore(&models.Document{
		ID: 1, Slug: "razbor", Title: "Второе", MarkdownContent: "второе тело",
		PublishedTitle: "Первое", PublishedMarkdown: "первое тело",
		PublishedAt: timePtr(time.Now()), WasPublished: true,
		AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		ReviewStatus: models.DocumentDraft,
	})
	h := NewDocumentReviewHandler(store, "секрет", false, nil)
	vars := map[string]string{"nickname": "чтец", "slug": "razbor"}

	rr := doPostVars(t, h.Submit, "/api/documents/чтец/razbor/submit", "",
		vars, readerClaims(5, "чтец"))
	if rr.Code != http.StatusOK {
		t.Fatalf("отправка: код %d (%s)", rr.Code, rr.Body.String())
	}
	if store.docs[1].ReviewStatus != models.DocumentPending {
		t.Fatalf("после отправки состояние %q", store.docs[1].ReviewStatus)
	}
	if store.docs[1].PublishedMarkdown != "первое тело" {
		t.Fatal("отправка подменила то, что на людях")
	}

	rr = doPostVars(t, h.Approve, "/api/documents/чтец/razbor/approve", "",
		vars, editorClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("одобрение: код %d", rr.Code)
	}
	if store.docs[1].PublishedMarkdown != "второе тело" {
		t.Fatalf("одобрение не сменило показываемое: %q", store.docs[1].PublishedMarkdown)
	}
}

func TestRejectNamesReasonAndKeepsShownEdition(t *testing.T) {
	store := newFakeDocumentStore(&models.Document{
		ID: 1, Slug: "razbor", Title: "Второе", MarkdownContent: "второе тело",
		PublishedTitle: "Первое", PublishedMarkdown: "первое тело",
		PublishedAt: timePtr(time.Now()), WasPublished: true,
		AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		ReviewStatus: models.DocumentPending,
	})
	h := NewDocumentReviewHandler(store, "секрет", false, nil)
	vars := map[string]string{"nickname": "чтец", "slug": "razbor"}

	// Причина вне закрытого списка — отказ, а не молчаливое «отклонено» без
	// причины: автору обязаны назвать, за что.
	rr := doPostVars(t, h.Reject, "/api/documents/чтец/razbor/reject",
		`{"reason":"не_понравилось"}`, vars, editorClaims())
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("причина вне списка принята: код %d", rr.Code)
	}

	rr = doPostVars(t, h.Reject, "/api/documents/чтец/razbor/reject",
		`{"reason":"не_по_теме"}`, vars, editorClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("отказ: код %d (%s)", rr.Code, rr.Body.String())
	}
	got := store.docs[1]
	if got.ReviewStatus != models.DocumentRejected {
		t.Fatalf("состояние после отказа %q", got.ReviewStatus)
	}
	if got.RejectReason == nil || *got.RejectReason != models.DocumentRejectOffTopic {
		t.Fatal("причина отказа не сохранена — автору нечего показать")
	}
	// Отказ касается правки, а не того, что уже одобрено и стоит на людях.
	if got.PublishedMarkdown != "первое тело" || got.PublishedAt == nil {
		t.Fatal("отказ снял с публикации прежнюю одобренную редакцию")
	}
}

func TestSubmitRefusesForeignDocument(t *testing.T) {
	doc := readerDoc(5, "чтец")
	doc.Slug = "razbor"
	store := newFakeDocumentStore(doc)
	h := NewDocumentReviewHandler(store, "секрет", false, nil)
	rr := doPostVars(t, h.Submit, "/api/documents/чтец/razbor/submit", "",
		map[string]string{"nickname": "чтец", "slug": "razbor"}, readerClaims(6, "другой"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("чужой разбор отправлен на модерацию: код %d", rr.Code)
	}
}

// Сотрудник не проходит модерацию собственного разбора: очередь, в которой
// модератор ждёт сам себя, — ритуал, а не контроль. Довод тот же, по
// которому CollectionHandler.Publish освобождает staff от предела частоты.
func TestStaffSubmitPublishesImmediately(t *testing.T) {
	store := newFakeDocumentStore(&models.Document{
		ID: 2, Slug: "redakcionnyi", Title: "Редакционный", MarkdownContent: "тело",
		AuthorNickname: "", ReviewStatus: models.DocumentDraft,
	})
	h := NewDocumentReviewHandler(store, "секрет", false, nil)
	rr := doPostVars(t, h.Submit, "/api/documents/redakcionnyi/submit", "",
		map[string]string{"slug": "redakcionnyi"}, editorClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("код %d", rr.Code)
	}
	if store.docs[2].PublishedAt == nil {
		t.Fatal("сотруднический разбор не опубликован сразу")
	}
	if store.docs[2].ReviewStatus != models.DocumentApproved {
		t.Fatalf("состояние %q", store.docs[2].ReviewStatus)
	}
	if store.submitted {
		t.Fatal("сотруднический разбор уехал в очередь модерации")
	}
}

// Снятие с публикации доступно владельцу и сотруднику, но правку чужого
// разбора не открывает.
func TestUnpublishIsTakedownNotEdit(t *testing.T) {
	newStore := func() *fakeDocumentStore {
		return newFakeDocumentStore(&models.Document{
			ID: 1, Slug: "razbor", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
			PublishedAt: timePtr(time.Now()), WasPublished: true,
			PublishedMarkdown: "тело", MarkdownContent: "черновик автора",
		})
	}
	vars := map[string]string{"nickname": "чтец", "slug": "razbor"}

	// Редакция снимает по жалобе.
	store := newStore()
	h := NewDocumentReviewHandler(store, "секрет", false, nil)
	if rr := doPostVars(t, h.Unpublish, "/api/documents/чтец/razbor/unpublish", "", vars, editorClaims()); rr.Code != http.StatusOK {
		t.Fatalf("снятие редактором: код %d", rr.Code)
	}
	if store.docs[1].PublishedAt != nil {
		t.Fatal("разбор остался на людях")
	}
	if store.docs[1].MarkdownContent != "черновик автора" {
		t.Fatal("снятие тронуло черновик автора")
	}
	if !store.docs[1].WasPublished {
		t.Fatal("след публикации стёрт — снятое не отличить от небывшего")
	}

	// Владелец убирает своё со стены.
	store = newStore()
	h = NewDocumentReviewHandler(store, "секрет", false, nil)
	if rr := doPostVars(t, h.Unpublish, "/api/documents/чтец/razbor/unpublish", "", vars, readerClaims(5, "чтец")); rr.Code != http.StatusOK {
		t.Fatalf("снятие владельцем: код %d", rr.Code)
	}

	// Посторонний читатель — нет.
	store = newStore()
	h = NewDocumentReviewHandler(store, "секрет", false, nil)
	if rr := doPostVars(t, h.Unpublish, "/api/documents/чтец/razbor/unpublish", "", vars, readerClaims(6, "другой")); rr.Code != http.StatusForbidden {
		t.Fatalf("посторонний снял чужой разбор: код %d", rr.Code)
	}
}

// I2 на уровне обработчиков: снятие с публикации отзывает заявку обеими
// дверями — и владельцем, и редакцией, — и снятый разбор уже нельзя принять
// из очереди. Сам SQL проверяется против настоящего Postgres
// (internal/repository/document_unpublish_test.go); здесь — что обработчик
// приёма после снятия отвечает отказом, а не публикует текст обратно.
func TestUnpublishWithdrawsSubmissionSoApproveCannotRepublish(t *testing.T) {
	pendingOverLive := func() *models.Document {
		return &models.Document{
			ID: 1, Slug: "razbor", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
			PublishedAt: timePtr(time.Now()), WasPublished: true,
			PublishedMarkdown: "прежняя редакция", MarkdownContent: "новая редакция",
			ReviewStatus: models.DocumentPending, SubmittedAt: timePtr(time.Now()),
		}
	}
	vars := map[string]string{"nickname": "чтец", "slug": "razbor"}

	for _, who := range []struct {
		name   string
		claims *auth.Claims
	}{
		{"редакция по жалобе", editorClaims()},
		{"владелец", readerClaims(5, "чтец")},
	} {
		t.Run(who.name, func(t *testing.T) {
			store := newFakeDocumentStore(pendingOverLive())
			h := NewDocumentReviewHandler(store, "секрет", false, nil)

			if rr := doPostVars(t, h.Unpublish, "/api/documents/чтец/razbor/unpublish", "", vars, who.claims); rr.Code != http.StatusOK {
				t.Fatalf("снятие: код %d — %s", rr.Code, rr.Body.String())
			}
			if got := store.docs[1].ReviewStatus; got != models.DocumentDraft {
				t.Errorf("заявка не отозвана: состояние %q", got)
			}
			if store.docs[1].MarkdownContent != "новая редакция" {
				t.Error("снятие тронуло черновик автора")
			}

			// Очередь модератора: строки больше нет.
			queue := doGetVars(t, h.Queue, "/api/documents/review", nil, editorClaims())
			if queue.Code != http.StatusOK {
				t.Fatalf("очередь: код %d", queue.Code)
			}
			var pending []models.Document
			if err := json.Unmarshal(queue.Body.Bytes(), &pending); err != nil {
				t.Fatalf("очередь не разобралась: %v", err)
			}
			for _, d := range pending {
				if d.ID == 1 {
					t.Error("снятый разбор остался в очереди модератора")
				}
			}

			// И «Принять» его уже не поднимает обратно на люди.
			rr := doPostVars(t, h.Approve, "/api/documents/чтец/razbor/approve", "", vars, editorClaims())
			if rr.Code != http.StatusConflict {
				t.Fatalf("приём снятого прошёл с кодом %d: %s", rr.Code, rr.Body.String())
			}
			if store.docs[1].PublishedAt != nil {
				t.Fatal("снятый по жалобе разбор вернулся на люди приёмом из очереди")
			}
		})
	}
}

// errorMessage разбирает {"message": "…"} — форму writeError.
func errorMessage(t *testing.T, body []byte) string {
	t.Helper()
	var resp struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("тело ошибки не разобралось: %v (%s)", err, body)
	}
	return resp.Message
}

// TestRejectAlreadyApprovedDocumentIsConflict — фикс-раунд 1 задачи 6,
// сценарий двух модераторов: один успел одобрить, у второго в устаревшей
// вкладке осталась кнопка «отклонить». Отклонить уже одобренное — 409, а не
// молчаливая перезапись чужого решения текстом «отклонено, но всё ещё на
// людях» (видимость смотрит на published_at, а не на review_status).
func TestRejectAlreadyApprovedDocumentIsConflict(t *testing.T) {
	store := newFakeDocumentStore(&models.Document{
		ID: 1, Slug: "razbor", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		Title: "Второе", MarkdownContent: "второе тело",
		PublishedTitle: "Второе", PublishedMarkdown: "второе тело",
		PublishedAt: timePtr(time.Now()), WasPublished: true,
		ReviewStatus: models.DocumentApproved,
	})
	h := NewDocumentReviewHandler(store, "секрет", false, nil)

	rr := doPostVars(t, h.Reject, "/api/documents/чтец/razbor/reject",
		`{"reason":"не_по_теме"}`, map[string]string{"nickname": "чтец", "slug": "razbor"}, editorClaims())
	if rr.Code != http.StatusConflict {
		t.Fatalf("отказ одобренному: код %d (%s)", rr.Code, rr.Body.String())
	}
	if got := errorMessage(t, rr.Body.Bytes()); got != "Разбор уже рассмотрен" {
		t.Fatalf("сообщение %q, ожидалось «Разбор уже рассмотрен»", got)
	}
	if store.docs[1].ReviewStatus != models.DocumentApproved {
		t.Fatal("отказ второго модератора перезаписал чужое решение")
	}
	if store.docs[1].PublishedAt == nil {
		t.Fatal("отклонённый ответ увёл документ с публикации без единой строки об этом")
	}
}

// TestApproveUnsubmittedDraftIsConflict — второй сценарий фикс-раунда 1
// задачи 6: одобрение черновика, который автор не отправлял, опубликовало бы
// приватный текст без его согласия.
func TestApproveUnsubmittedDraftIsConflict(t *testing.T) {
	store := newFakeDocumentStore(&models.Document{
		ID: 1, Slug: "razbor", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		Title: "Черновик", MarkdownContent: "тело",
		ReviewStatus: models.DocumentDraft,
	})
	h := NewDocumentReviewHandler(store, "секрет", false, nil)

	rr := doPostVars(t, h.Approve, "/api/documents/чтец/razbor/approve", "",
		map[string]string{"nickname": "чтец", "slug": "razbor"}, editorClaims())
	if rr.Code != http.StatusConflict {
		t.Fatalf("одобрение неотправленного черновика: код %d (%s)", rr.Code, rr.Body.String())
	}
	if got := errorMessage(t, rr.Body.Bytes()); got != "Разбор не подавался на проверку" {
		t.Fatalf("сообщение %q, ожидалось «Разбор не подавался на проверку»", got)
	}
	if store.docs[1].PublishedAt != nil {
		t.Fatal("черновик опубликован без отправки — приватный текст ушёл наружу без согласия автора")
	}
}

// TestApproveRaceWithStaleHandlerCheckIsConflict — второй, атомарный уровень
// guard'а: если состояние сменилось МЕЖДУ проверкой на уровне обработчика и
// записью (гонка двух модераторов), repository.ErrDocumentNotPending обязан
// дойти до ответа тем же 409 «Разбор уже рассмотрен», а не 500. Гонку
// подделывает approveErr фейка — сам фейк-стор не умеет две параллельные
// записи, но обязан правильно ПЕРЕВЕСТИ ошибку репозитория в код ответа.
func TestApproveRaceWithStaleHandlerCheckIsConflict(t *testing.T) {
	store := newFakeDocumentStore(&models.Document{
		ID: 1, Slug: "razbor", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		Title: "Второе", MarkdownContent: "второе тело",
		ReviewStatus: models.DocumentPending,
	})
	store.approveErr = repository.ErrDocumentNotPending
	h := NewDocumentReviewHandler(store, "секрет", false, nil)

	rr := doPostVars(t, h.Approve, "/api/documents/чтец/razbor/approve", "",
		map[string]string{"nickname": "чтец", "slug": "razbor"}, editorClaims())
	if rr.Code != http.StatusConflict {
		t.Fatalf("гонка модераторов: код %d (%s), ожидался 409", rr.Code, rr.Body.String())
	}
	if got := errorMessage(t, rr.Body.Bytes()); got != "Разбор уже рассмотрен" {
		t.Fatalf("сообщение %q, ожидалось «Разбор уже рассмотрен»", got)
	}
}

// TestUnpublishAlreadyUnpublishedIsConflict — фикс-раунд 1 задачи 6, фикс 3:
// снять с публикации то, чего на ней уже нет, — бессмысленное действие и,
// заодно, единственная поверхность, на которой сотрудник тыкался бы в чужой
// неопубликованный черновик через Unpublish.
func TestUnpublishAlreadyUnpublishedIsConflict(t *testing.T) {
	store := newFakeDocumentStore(&models.Document{
		ID: 1, Slug: "razbor", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		PublishedAt: nil, WasPublished: true,
	})
	h := NewDocumentReviewHandler(store, "секрет", false, nil)

	rr := doPostVars(t, h.Unpublish, "/api/documents/чтец/razbor/unpublish", "",
		map[string]string{"nickname": "чтец", "slug": "razbor"}, editorClaims())
	if rr.Code != http.StatusConflict {
		t.Fatalf("снятие неопубликованного: код %d (%s)", rr.Code, rr.Body.String())
	}
}

// Задача 10 (находка приёмки, .scratch/razbor-vkleyka/issues/03-…): снятие с
// публикации отвечает 410 из базы немедленно, но страница краулера и карточка
// og:image живут в отдельном кэше в памяти (internal/seo) без своего пути
// инвалидации — без явного сброса снятый разбор отдавался бы им ещё час
// (страница) и сутки (карточка). Наблюдаемое здесь — что Unpublish зовёт
// сброс краулерского кэша, а не как устроен сам fakeCrawlerCaches — тот же
// подставной счётчик, что уже проверяет снос тома
// (work_delete_cache_test.go).
func TestUnpublishPurgesCrawlerCache(t *testing.T) {
	store := newFakeDocumentStore(&models.Document{
		ID: 1, Slug: "razbor", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		PublishedAt: timePtr(time.Now()), WasPublished: true,
		PublishedMarkdown: "тело", MarkdownContent: "тело",
	})
	crawler := &fakeCrawlerCaches{}
	h := NewDocumentReviewHandler(store, "секрет", false, NewServingCache(nil, crawler))

	rr := doPostVars(t, h.Unpublish, "/api/documents/чтец/razbor/unpublish", "",
		map[string]string{"nickname": "чтец", "slug": "razbor"}, editorClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("снятие: код %d (%s)", rr.Code, rr.Body.String())
	}
	if crawler.calls != 1 {
		t.Errorf("краулерский кэш сброшен %d раз, ожидался 1", crawler.calls)
	}
}

// TestRespondWithFiltersForeignDraftOnUnpublish — фикс-раунд 1 задачи 6,
// фикс 2. Проверено тестом, а не рассуждением: сотрудник снимает с публикации
// ЧУЖОЙ разбор (рычаг снятия по жалобе), который ни разу не отправлялся на
// рассмотрение — mayReadDraft для него возвращает false, а respondWith,
// перечитав документ, обязан отдать ОДОБРЕННУЮ редакцию, а не сырой
// приватный черновик.
func TestRespondWithFiltersForeignDraftOnUnpublish(t *testing.T) {
	store := newFakeDocumentStore(&models.Document{
		ID: 1, Slug: "razbor", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		Title: "Приватная правка", MarkdownContent: "приватное тело",
		PublishedTitle: "Одобренное", PublishedMarkdown: "одобренное тело",
		PublishedAt: timePtr(time.Now()), WasPublished: true,
		ReviewStatus: models.DocumentDraft,
	})
	h := NewDocumentReviewHandler(store, "секрет", false, nil)

	rr := doPostVars(t, h.Unpublish, "/api/documents/чтец/razbor/unpublish", "",
		map[string]string{"nickname": "чтец", "slug": "razbor"}, editorClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("снятие редактором: код %d (%s)", rr.Code, rr.Body.String())
	}
	var resp models.Document
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("тело ответа не разобралось: %v", err)
	}
	if resp.Title == "Приватная правка" || resp.MarkdownContent == "приватное тело" {
		t.Fatalf("ответ утёк приватный черновик постороннему сотруднику: %+v", resp)
	}
	if resp.Title != "Одобренное" || resp.MarkdownContent != "одобренное тело" {
		t.Fatalf("ответ не показал одобренную редакцию: %+v", resp)
	}
	if resp.ReviewStatus != "" {
		t.Fatalf("поля модерации не погашены для постороннего: review_status=%q", resp.ReviewStatus)
	}
}

// TestApproveResponseShowsModeratorTheirOwnDecision — фикс-раунд 2 задачи 6.
// Проверено телом ответа, а не состоянием фейка: починка 2 (фильтр
// documentForViewer в respondWith) была рассчитана на Unpublish и, применённая
// к Approve, гасила поля модерации в ОТВЕТЕ САМОМУ МОДЕРАТОРУ — после
// одобрения review_status уже не «на_рассмотрении», mayReadDraft для
// не-автора возвращает false, и фильтр стирал бы дату решения из ответа тому,
// кто её только что проставил.
func TestApproveResponseShowsModeratorTheirOwnDecision(t *testing.T) {
	store := newFakeDocumentStore(&models.Document{
		ID: 1, Slug: "razbor", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		Title: "Второе", MarkdownContent: "второе тело",
		PublishedTitle: "Первое", PublishedMarkdown: "первое тело",
		PublishedAt: timePtr(time.Now()), WasPublished: true,
		ReviewStatus: models.DocumentPending,
	})
	h := NewDocumentReviewHandler(store, "секрет", false, nil)

	rr := doPostVars(t, h.Approve, "/api/documents/чтец/razbor/approve", "",
		map[string]string{"nickname": "чтец", "slug": "razbor"}, editorClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("одобрение: код %d (%s)", rr.Code, rr.Body.String())
	}
	var resp models.Document
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("тело ответа не разобралось: %v", err)
	}
	if resp.ReviewStatus != models.DocumentApproved {
		t.Fatalf("ответ модератору не показал исход его же решения: review_status=%q", resp.ReviewStatus)
	}
	if resp.ReviewedAt == nil {
		t.Fatal("ответ модератору погасил дату решения, которое он только что вынес")
	}
	if resp.ModeratorID == nil || *resp.ModeratorID != editorClaims().UserID {
		t.Fatal("ответ не назвал модератора, вынесшего решение")
	}
}

// TestRejectResponseShowsModeratorTheReason — зеркало предыдущего теста для
// Reject: модератор, только что назвавший причину отказа, обязан получить её
// обратно в подтверждении, а не увидеть погашенные поля.
func TestRejectResponseShowsModeratorTheReason(t *testing.T) {
	store := newFakeDocumentStore(&models.Document{
		ID: 1, Slug: "razbor", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		Title: "Второе", MarkdownContent: "второе тело",
		PublishedTitle: "Первое", PublishedMarkdown: "первое тело",
		PublishedAt: timePtr(time.Now()), WasPublished: true,
		ReviewStatus: models.DocumentPending,
	})
	h := NewDocumentReviewHandler(store, "секрет", false, nil)

	rr := doPostVars(t, h.Reject, "/api/documents/чтец/razbor/reject",
		`{"reason":"не_по_теме"}`, map[string]string{"nickname": "чтец", "slug": "razbor"}, editorClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("отказ: код %d (%s)", rr.Code, rr.Body.String())
	}
	var resp models.Document
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("тело ответа не разобралось: %v", err)
	}
	if resp.ReviewStatus != models.DocumentRejected {
		t.Fatalf("ответ модератору не показал исход его же решения: review_status=%q", resp.ReviewStatus)
	}
	if resp.RejectReason == nil || *resp.RejectReason != models.DocumentRejectOffTopic {
		t.Fatal("ответ модератору погасил причину отказа, которую он только что назвал")
	}
	if resp.ReviewedAt == nil {
		t.Fatal("ответ модератору погасил дату решения")
	}
}
