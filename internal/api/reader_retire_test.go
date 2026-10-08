package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proofreader/internal/models"
)

// I4. Уход НЕ освобождает ник.
//
// Уход сносил строку, ник освобождался, следующий желающий его занимал — и
// опубликованный чужой разбор продолжал печатать «Собрал читатель ‹ник›»,
// теперь уже про живого человека, который этого не писал. Правку мы закрыли
// раньше (сличение по владельцу, mayEditDocument), но закрыли одну половину:
// КТО ПРАВИТ. Вторая — КОГО ПОДПИСАЛИ — оставалась открытой.
//
// Проект уже запрещает гомоглифы в никах ровно с этим доводом («гомоглиф
// иначе подписывает чужой текст чужим именем», models.ValidateNickname). Уход
// приводил к тому же исходу другой дверью.
func TestReaderLeavingRetiresTheirNickname(t *testing.T) {
	users := newFakeAuthUserStore()
	svc := newTestAuthService()
	nick := "уходящий"
	users.addUser(t, svc, models.RoleReader, nick, "пароль-читателя")
	h := NewAuthHandler(users, svc, &fakeAuthAttemptStore{}, "test-secret", false)

	rec := httptest.NewRecorder()
	withClaims(readerClaims(1, nick), h.DeleteMe)(
		rec, httptest.NewRequest(http.MethodDelete, "/api/auth/me", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("уход: код %d (%s)", rec.Code, rec.Body.String())
	}

	// Тот же ник занять заново нельзя — и отказ объясняет причину словами, а
	// не выглядит обычной ошибкой входа.
	rec = httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest(nick, "совсем-другой-пароль"))
	if rec.Code == http.StatusOK {
		t.Fatalf("ушедший ник занят заново: %s", rec.Body.String())
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("код %d, ожидался 409 — отличимый от 401 «пароль не подошёл»: %s",
			rec.Code, rec.Body.String())
	}
	msg := decodeErrorMessage(t, rec.Body.Bytes())
	if strings.Contains(msg, "пароль не подошёл") {
		t.Errorf("отказ неотличим от «пароль не подошёл»: %q", msg)
	}
	if !strings.Contains(msg, "тексты") {
		t.Errorf("отказ не объясняет причину человеческими словами: %q", msg)
	}
	if _, err := users.GetByNickname(context.Background(), nick); err == nil {
		t.Error("под ушедшим ником завелась новая учётная запись")
	}

	// Регистр и NFKC ничего не меняют: отставка держится тем же ключом, что и
	// уникальность ника в Postgres.
	rec = httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest("УХОДЯЩИЙ", "совсем-другой-пароль"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("ушедший ник занят в другом регистре: код %d (%s)", rec.Code, rec.Body.String())
	}

	// Контрольная группа: посторонний ник заводится как раньше — отказ выше
	// держится на отставке, а не на чём-то, что сломало дверь целиком.
	rec = httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest("новичок", "совсем-другой-пароль"))
	if rec.Code != http.StatusOK {
		t.Fatalf("отставка задела посторонний ник: код %d (%s)", rec.Code, rec.Body.String())
	}
}

// M8. Токен переживает удаление учётной записи, поэтому второй DELETE
// достижим обычным двойным нажатием. Отвечать на него 500 нечестно: ничего
// не сломалось, учётной записи просто уже нет.
func TestDeleteMeTwiceSaysAlreadyGoneNotServerError(t *testing.T) {
	users := newFakeAuthUserStore()
	svc := newTestAuthService()
	nick := "дважды"
	users.addUser(t, svc, models.RoleReader, nick, "пароль-читателя")
	h := NewAuthHandler(users, svc, &fakeAuthAttemptStore{}, "test-secret", false)

	leave := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		withClaims(readerClaims(1, nick), h.DeleteMe)(
			rec, httptest.NewRequest(http.MethodDelete, "/api/auth/me", nil))
		return rec
	}

	if rec := leave(); rec.Code != http.StatusNoContent {
		t.Fatalf("первый уход: код %d (%s)", rec.Code, rec.Body.String())
	}
	rec := leave()
	if rec.Code == http.StatusInternalServerError {
		t.Fatalf("повторный уход отвечает сбоем сервера: %s", rec.Body.String())
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("повторный уход: код %d, ожидался 404 «уже нет»", rec.Code)
	}
	if msg := decodeErrorMessage(t, rec.Body.Bytes()); !strings.Contains(msg, "уже нет") {
		t.Errorf("повторный уход не говорит «уже нет»: %q", msg)
	}
}
