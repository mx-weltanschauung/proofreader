package api

import (
	"net/http"

	"proofreader/internal/auth"
	"proofreader/internal/models"
)

// isStaffClaims — сотрудник ли пришедший. Проверяется РОЛЬ, а не факт входа:
// тем же JWT_SECRET, которым подписан токен администратора, подписан теперь
// и токен любого постороннего.
func isStaffClaims(claims *auth.Claims) bool {
	return claims != nil &&
		(claims.Role == models.RoleEditor || claims.Role == models.RoleAdministrator)
}

// mayEditDocument решает, вправе ли пришедший править этот разбор.
//
// Различие идёт по AuthorNickname, а НЕ по OwnerID — дословно тот же довод,
// что у mayEdit подборок: пустой ник и есть признак сотруднического разбора,
// а OwnerID законно бывает nil и у читательской строки (ON DELETE SET NULL
// при удалении учётной записи). Смотри правило на OwnerID == nil — и
// удаление аккаунта автора молча превращало бы его разбор в сотруднический,
// который правит любой редактор. Различая по нику, осиротевшая строка просто
// перестаёт быть редактируемой вовсе, что и есть правильный исход.
//
// Редактор чужой (читательский) разбор не правит. Ему остаётся снятие с
// публикации — рычаг снятия, а не правки (DocumentReviewHandler.Unpublish).
func mayEditDocument(claims *auth.Claims, d *models.Document) bool {
	if claims == nil {
		return false
	}
	if d.AuthorNickname == "" {
		return isStaffClaims(claims)
	}
	return d.OwnerID != nil && *d.OwnerID == claims.UserID
}

// mayReadDraft решает, вправе ли пришедший видеть ЧЕРНОВИК — то, что ещё не
// одобрено.
//
// Модератор читает поданное, и только его: неотправленный черновик приватен
// и от редактора тоже (решение 07 — «черновик приватен по умолчанию»), а
// судить о том, что подали, иначе не о чем.
func mayReadDraft(claims *auth.Claims, d *models.Document) bool {
	if mayEditDocument(claims, d) {
		return true
	}
	return isStaffClaims(claims) && d.ReviewStatus == models.DocumentPending
}

// documentVisibleTo решает, вправе ли пришедший вообще получить этот разбор.
// Один в один collectionVisibleTo, и звать его обязаны ВСЕ маршруты,
// отдающие тело разбора наружу: Get, View и догрузка вклейки (Full) — иначе
// снятый разбор остаётся доступен по одному из них (так уже случалось у
// подборок: ItemPages и download отдавали 200 после 410 на Get).
func documentVisibleTo(claims *auth.Claims, d *models.Document) (status int, message string, ok bool) {
	if d.PublishedAt != nil {
		return 0, "", true
	}
	if mayReadDraft(claims, d) {
		return 0, "", true
	}
	if !d.WasPublished {
		// Никогда не публиковался — черновик не выдаёт даже факт своего
		// существования.
		return http.StatusNotFound, "Разбор не найден", false
	}
	return http.StatusGone, "Разбор снят с публикации", false
}

// documentForViewer отдаёт разбор так, как его вправе видеть пришедший.
//
// Тому, кто видит черновик (автор; модератор — у поданного), — как есть.
// Всем остальным Title/MarkdownContent подменяются ОДОБРЕННОЙ редакцией, а
// поля модерации гасятся: ждущая одобрения правка не должна выходить наружу
// ни целиком, ни намёком, иначе модерация — театр (пропустить безобидное,
// потом переписать).
//
// Возвращает КОПИЮ: исходная строка идёт дальше по обработчику и другим
// читателям, испортить её нельзя.
func documentForViewer(claims *auth.Claims, d *models.Document) *models.Document {
	if mayReadDraft(claims, d) {
		return d
	}
	shown := *d
	shown.Title = d.PublishedTitle
	shown.MarkdownContent = d.PublishedMarkdown
	shown.PublishedTitle = ""
	shown.PublishedMarkdown = ""
	shown.ReviewStatus = ""
	shown.RejectReason = nil
	shown.ModeratorID = nil
	shown.ReviewedAt = nil
	shown.SubmittedAt = nil
	return &shown
}
