package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
)

// fakeAuthUserStore — подставной склад пользователей для обеих дверей.
//
// GetByNickname на отсутствующем нике отвечает (nil, error), а не (nil, nil)
// — той же формой, что настоящий repository.UserRepository.GetByNickname.
// Фейк, врущий формой, уже давал в этом проекте два зелёных теста при
// сломанном боевом (см. CLAUDE.md).
type fakeAuthUserStore struct {
	byNicknameKey map[string]*models.User
	byEmail       map[string]*models.User
	created       []*models.User

	signupBlocked bool // предел заведения учёток по адресу исчерпан
	createErr     error

	// deleted — id, снятые через DeleteReader. Проверяется тестами вместо
	// заглядывания в f.created напрямую: явное имя читается как утверждение
	// «эта учётка ушла», а не как деталь реализации фейка.
	deleted map[int64]bool

	// retiredKeys — отставленные ники, по нормализованному ключу. Настоящий
	// репозиторий пишет их ТОЙ ЖЕ транзакцией, что сносит строку читателя
	// (I4), и фейк обязан вести себя так же: фейк, врущий формой, уже давал
	// в этом проекте два зелёных теста при сломанном боевом.
	retiredKeys map[string]bool
}

func newFakeAuthUserStore() *fakeAuthUserStore {
	return &fakeAuthUserStore{
		byNicknameKey: make(map[string]*models.User),
		byEmail:       make(map[string]*models.User),
	}
}

func (f *fakeAuthUserStore) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	u, ok := f.byEmail[email]
	if !ok {
		return nil, fmt.Errorf("user not found")
	}
	return u, nil
}

func (f *fakeAuthUserStore) GetByNickname(ctx context.Context, nickname string) (*models.User, error) {
	u, ok := f.byNicknameKey[models.NormalizeNickname(nickname)]
	if !ok {
		return nil, fmt.Errorf("user not found")
	}
	return u, nil
}

// GetByID ищет по f.created, а не по отдельной карте: и CreateReaderWithinLimit,
// и addUser уже кладут туда каждого заведённого пользователя. removeUser
// (ниже) вынимает строку оттуда — так тесты изображают удалённого читателя.
func (f *fakeAuthUserStore) GetByID(ctx context.Context, id int64) (*models.User, error) {
	for _, u := range f.created {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, fmt.Errorf("user not found")
}

// removeUser изображает удалённую учётную запись: убирает строку из всех
// мест, где её мог бы найти фейк, — ровно то, что делает настоящий DELETE.
func (f *fakeAuthUserStore) removeUser(id int64) {
	for i, u := range f.created {
		if u.ID == id {
			f.created = append(f.created[:i], f.created[i+1:]...)
			break
		}
	}
	for k, u := range f.byNicknameKey {
		if u.ID == id {
			delete(f.byNicknameKey, k)
		}
	}
}

// DeleteReader — тот же снаряд роли, что и в настоящем репозитории: строка
// сотрудника не уходит этим методом, даже если её id найден.
func (f *fakeAuthUserStore) DeleteReader(ctx context.Context, id int64) error {
	var found *models.User
	for _, u := range f.created {
		if u.ID == id {
			found = u
			break
		}
	}
	if found == nil || found.Role != models.RoleReader {
		return fmt.Errorf("reader not found")
	}
	// Отставка ника и снятие строки — одно действие, как в настоящем
	// репозитории (там — одна транзакция).
	if found.Nickname != nil && *found.Nickname != "" {
		if f.retiredKeys == nil {
			f.retiredKeys = make(map[string]bool)
		}
		f.retiredKeys[models.NormalizeNickname(*found.Nickname)] = true
	}
	f.removeUser(id)
	if f.deleted == nil {
		f.deleted = make(map[int64]bool)
	}
	f.deleted[id] = true
	return nil
}

func (f *fakeAuthUserStore) IsNicknameRetired(ctx context.Context, nickname string) (bool, error) {
	return f.retiredKeys[models.NormalizeNickname(nickname)], nil
}

func (f *fakeAuthUserStore) CreateReaderWithinLimit(
	ctx context.Context, user *models.User, limit int, since time.Time,
) (bool, error) {
	if f.createErr != nil {
		return false, f.createErr
	}
	if f.signupBlocked {
		return false, nil
	}
	user.ID = int64(len(f.created) + 1)
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()
	f.created = append(f.created, user)
	if user.Nickname != nil {
		f.byNicknameKey[models.NormalizeNickname(*user.Nickname)] = user
	}
	return true, nil
}

// addUser предзасевает пользователя с известным паролем — образец для тестов
// «такой уже есть». Роль передаётся явно: мутационный тест заводит так
// сотрудника с ником, которого в жизни не бывает, но фейк это позволяет.
func (f *fakeAuthUserStore) addUser(t *testing.T, authService *auth.Service, role models.UserRole, nickname, password string) *models.User {
	t.Helper()
	hash, err := authService.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	nick := nickname
	u := &models.User{
		ID: int64(len(f.created) + 1), Role: role, Nickname: &nick, PasswordHash: hash,
	}
	f.created = append(f.created, u)
	f.byNicknameKey[models.NormalizeNickname(nickname)] = u
	return u
}

// fakeAuthAttemptStore — подставной рельс попыток входа. Запоминает, с
// какими (ipHash, nickname) его звали, чтобы тесты могли проверить, что
// обработчик передаёт пару целиком, а не голый адрес.
type fakeAuthAttemptStore struct {
	blocked bool // ReserveAttempt отвечает «нельзя»

	reserveCalls []authAttemptCall
	clearCalls   []authAttemptCall
}

type authAttemptCall struct {
	ipHash, nickname string
}

func (f *fakeAuthAttemptStore) ReserveAttempt(
	ctx context.Context, ipHash, nickname string, limit int, since time.Time,
) (bool, error) {
	f.reserveCalls = append(f.reserveCalls, authAttemptCall{ipHash, nickname})
	if f.blocked {
		return false, nil
	}
	return true, nil
}

func (f *fakeAuthAttemptStore) Clear(ctx context.Context, ipHash, nickname string) error {
	f.clearCalls = append(f.clearCalls, authAttemptCall{ipHash, nickname})
	return nil
}

func newTestAuthService() *auth.Service {
	return auth.NewService(&config.JWTConfig{
		Secret:            "test-secret",
		Expiration:        time.Hour,
		RefreshExpiration: 24 * time.Hour,
		ReaderExpiration:  90 * 24 * time.Hour,
	})
}

// createTestAuthHandler собирает обработчик на пустых подставных складах —
// образец для тестов, которым нужен только Login/Me.
func createTestAuthHandler() *AuthHandler {
	return NewAuthHandler(newFakeAuthUserStore(), newTestAuthService(), &fakeAuthAttemptStore{}, "test-secret", false)
}

func readerLoginRequest(nickname, password string) *http.Request {
	body, _ := json.Marshal(ReaderLoginRequest{Nickname: nickname, Password: password})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reader", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func decodeErrorMessage(t *testing.T, body []byte) string {
	t.Helper()
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("тело ошибки не JSON: %s", body)
	}
	return payload.Message
}

func TestNewAuthHandler(t *testing.T) {
	authService := newTestAuthService()
	attempts := &fakeAuthAttemptStore{}

	handler := NewAuthHandler(newFakeAuthUserStore(), authService, attempts, "test-secret", false)
	if handler == nil {
		t.Fatal("Expected handler to be created")
	}
	if handler.authService != authService {
		t.Error("Expected authService to be set")
	}
}

func TestAuthHandler_Login_InvalidJSON(t *testing.T) {
	handler := createTestAuthHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString("invalid json"))
	rec := httptest.NewRecorder()

	handler.Login(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAuthHandler_Me_NoContext(t *testing.T) {
	handler := createTestAuthHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	rec := httptest.NewRecorder()

	handler.Me(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthHandler_Me_WithContext(t *testing.T) {
	handler := createTestAuthHandler()

	claims := &auth.Claims{
		UserID: 123,
		Email:  "test@example.com",
		Role:   models.RoleEditor,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.Me(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	var response UserResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.ID != claims.UserID {
		t.Errorf("ID = %d, want %d", response.ID, claims.UserID)
	}
	if response.Email != claims.Email {
		t.Errorf("Email = %s, want %s", response.Email, claims.Email)
	}
	if response.Role != claims.Role {
		t.Errorf("Role = %s, want %s", response.Role, claims.Role)
	}
}

func TestAuthHandler_Me_WrongContextType(t *testing.T) {
	handler := createTestAuthHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, "wrong type")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.Me(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestLoginRequest(t *testing.T) {
	req := LoginRequest{
		Email:    "test@example.com",
		Password: "password123",
	}

	if req.Email != "test@example.com" {
		t.Errorf("Email = %s, want test@example.com", req.Email)
	}
}

func TestAuthResponse(t *testing.T) {
	resp := AuthResponse{
		User: UserResponse{
			ID:    1,
			Email: "test@example.com",
			Role:  models.RoleEditor,
		},
		Token:        "access-token",
		RefreshToken: "refresh-token",
	}

	if resp.Token != "access-token" {
		t.Errorf("Token = %s, want access-token", resp.Token)
	}
	if resp.User.ID != 1 {
		t.Errorf("User.ID = %d, want 1", resp.User.ID)
	}
}

func TestUserResponse(t *testing.T) {
	resp := UserResponse{
		ID:    42,
		Email: "user@test.com",
		Role:  models.RoleAdministrator,
	}

	if resp.ID != 42 {
		t.Errorf("ID = %d, want 42", resp.ID)
	}
	if resp.Email != "user@test.com" {
		t.Errorf("Email = %s, want user@test.com", resp.Email)
	}
	if resp.Role != models.RoleAdministrator {
		t.Errorf("Role = %s, want administrator", resp.Role)
	}
}

// --- Дверь читателя: POST /api/auth/reader ---

func TestReaderLoginCreatesAccountWhenAbsent(t *testing.T) {
	users := newFakeAuthUserStore()
	attempts := &fakeAuthAttemptStore{}
	h := NewAuthHandler(users, newTestAuthService(), attempts, "test-secret", false)

	rec := httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest("Читатель1", "надёжный-пароль"))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}

	var resp AuthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if resp.Token == "" || resp.RefreshToken == "" {
		t.Fatal("токены не выданы")
	}
	if resp.User.Nickname == nil || *resp.User.Nickname != "Читатель1" {
		t.Fatalf("ник в ответе %v, ожидался Читатель1", resp.User.Nickname)
	}
	if resp.User.Role != models.RoleReader {
		t.Fatalf("роль в ответе %q, ожидалась %q", resp.User.Role, models.RoleReader)
	}

	if len(users.created) != 1 {
		t.Fatalf("ожидался один заведённый читатель, создано %d", len(users.created))
	}
	if users.created[0].Role != models.RoleReader {
		t.Fatalf("заведённый пользователь получил роль %q", users.created[0].Role)
	}
}

func TestReaderLoginTrimsNicknameBeforeStoring(t *testing.T) {
	// NormalizeNickname в Go тримит пробелы, а вычисляемый столбец
	// nickname_key в Postgres — нет: записав сырую строку, разошлись бы
	// молча. Проверяем, что в хранилище уходит уже триммленная форма.
	users := newFakeAuthUserStore()
	attempts := &fakeAuthAttemptStore{}
	h := NewAuthHandler(users, newTestAuthService(), attempts, "test-secret", false)

	rec := httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest("  Читатель2  ", "надёжный-пароль"))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	if len(users.created) != 1 {
		t.Fatalf("ожидался один заведённый читатель, создано %d", len(users.created))
	}
	if got := *users.created[0].Nickname; got != "Читатель2" {
		t.Fatalf("в хранилище ушёл ник %q, ожидался %q без пробелов", got, "Читатель2")
	}
}

func TestReaderLoginRejectsWrongPassword(t *testing.T) {
	users := newFakeAuthUserStore()
	authService := newTestAuthService()
	users.addUser(t, authService, models.RoleReader, "Читатель3", "правильный-пароль")
	attempts := &fakeAuthAttemptStore{}
	h := NewAuthHandler(users, authService, attempts, "test-secret", false)

	rec := httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest("Читатель3", "неверный-пароль"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ожидался 401, получено %d: %s", rec.Code, rec.Body.String())
	}
	msg := decodeErrorMessage(t, rec.Body.Bytes())
	if !strings.Contains(msg, "уже есть") {
		t.Fatalf("отказ не про существующего читателя: %q", msg)
	}
	if len(users.created) != 1 {
		t.Fatalf("неверный пароль не должен заводить нового читателя, в хранилище %d", len(users.created))
	}
}

func TestReaderLoginRejectsBadNickname(t *testing.T) {
	h := NewAuthHandler(newFakeAuthUserStore(), newTestAuthService(), &fakeAuthAttemptStore{}, "test-secret", false)

	rec := httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest("два слова", "надёжный-пароль"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидался 400, получено %d: %s", rec.Code, rec.Body.String())
	}
	if msg := decodeErrorMessage(t, rec.Body.Bytes()); msg == "" {
		t.Fatal("в теле ошибки нет message — читатель увидит запасную фразу")
	}
}

func TestReaderLoginRejectsShortPassword(t *testing.T) {
	h := NewAuthHandler(newFakeAuthUserStore(), newTestAuthService(), &fakeAuthAttemptStore{}, "test-secret", false)

	rec := httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest("Читатель4", "1234567"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидался 400, получено %d: %s", rec.Code, rec.Body.String())
	}
	if msg := decodeErrorMessage(t, rec.Body.Bytes()); msg == "" {
		t.Fatal("в теле ошибки нет message")
	}
}

// TestReaderLoginRejectsLongPassword — рецензия: bcrypt отказывает паролю
// длиннее 72 байт ошибкой, которая до этой правки доезжала до читателя как
// голый 500 «Не удалось завести читателя» в точке невозврата — ник уже занят
// этим же запросом, откатить регистрацию нечем. Проверяет и границу (72 байта
// — ещё можно), и первый шаг за неё (73 — уже нельзя).
func TestReaderLoginRejectsLongPassword(t *testing.T) {
	h := NewAuthHandler(newFakeAuthUserStore(), newTestAuthService(), &fakeAuthAttemptStore{}, "test-secret", false)

	rec := httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest("Читатель6", strings.Repeat("a", 73)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидался 400 на пароле в 73 байта, получено %d: %s", rec.Code, rec.Body.String())
	}
	msg := decodeErrorMessage(t, rec.Body.Bytes())
	if msg == "" {
		t.Fatal("в теле ошибки нет message")
	}
	if !strings.Contains(msg, "72") {
		t.Errorf("сообщение не называет предел: %q", msg)
	}
}

// TestReaderLoginAcceptsPasswordAtByteLimit — 72 байта ровно на границе
// должны заводить читателя как обычно, а не спотыкаться о тот же отказ.
func TestReaderLoginAcceptsPasswordAtByteLimit(t *testing.T) {
	users := newFakeAuthUserStore()
	h := NewAuthHandler(users, newTestAuthService(), &fakeAuthAttemptStore{}, "test-secret", false)

	rec := httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest("Читатель7", strings.Repeat("a", 72)))

	if rec.Code != http.StatusOK {
		t.Fatalf("ожидался 200 на пароле ровно в 72 байта, получено %d: %s", rec.Code, rec.Body.String())
	}
	if len(users.created) != 1 {
		t.Fatalf("читатель не заведён: %d", len(users.created))
	}
}

// TestReaderLoginRejectsLongMultibytePassword — та же граница, но байтами, а
// не знаками: кириллица занимает по два байта на букву, поэтому 40 букв уже
// превышают предел в 72 байта, хотя рун в пароле меньше 72.
func TestReaderLoginRejectsLongMultibytePassword(t *testing.T) {
	h := NewAuthHandler(newFakeAuthUserStore(), newTestAuthService(), &fakeAuthAttemptStore{}, "test-secret", false)

	rec := httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest("Читатель8", strings.Repeat("ж", 40)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидался 400 на 40 буквах кириллицы (80 байт), получено %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReaderLoginStopsAtAttemptLimit(t *testing.T) {
	users := newFakeAuthUserStore()
	attempts := &fakeAuthAttemptStore{blocked: true}
	h := NewAuthHandler(users, newTestAuthService(), attempts, "test-secret", false)

	rec := httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest("Читатель5", "надёжный-пароль"))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("ожидался 429, получено %d: %s", rec.Code, rec.Body.String())
	}
	if len(users.created) != 0 {
		t.Fatal("читатель заведён при исчерпанном пределе попыток")
	}
}

// TestReaderLoginAttemptKeyIsAddressAndNicknamePair — рельс считает попытки
// парой (адрес, ник), а не голым адресом: иначе подбирающий делает девять
// попыток к чужому нику, десятой входит в собственную учётку, Clear обнуляет
// счёт — и так без конца (docblock ReserveAttempt). Обработчик обязан
// передавать ник в оба вызова, а не только адрес.
func TestReaderLoginAttemptKeyIsAddressAndNicknamePair(t *testing.T) {
	users := newFakeAuthUserStore()
	authService := newTestAuthService()
	users.addUser(t, authService, models.RoleReader, "Читатель6", "надёжный-пароль")
	attempts := &fakeAuthAttemptStore{}
	h := NewAuthHandler(users, authService, attempts, "test-secret", false)

	rec := httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest("Читатель6", "надёжный-пароль"))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	if len(attempts.reserveCalls) != 1 || attempts.reserveCalls[0].nickname != "Читатель6" {
		t.Fatalf("ReserveAttempt вызван без ника: %+v", attempts.reserveCalls)
	}
	if len(attempts.clearCalls) != 1 || attempts.clearCalls[0].nickname != "Читатель6" {
		t.Fatalf("Clear вызван без ника: %+v", attempts.clearCalls)
	}
	if attempts.reserveCalls[0].ipHash == "" {
		t.Fatal("отметка адреса не посчитана")
	}
}

// TestReaderLoginStaffCannotUseReaderDoor — мутационная проверка: заводит
// сотрудника (роль editor) с ником и пробует войти им через читательскую
// дверь. Без ветки `if user.Role != models.RoleReader` в ReaderLogin верный
// пароль сотрудника прошёл бы её и выдал бы читательский (90-суточный) токен
// вместо отказа.
func TestReaderLoginStaffCannotUseReaderDoor(t *testing.T) {
	users := newFakeAuthUserStore()
	authService := newTestAuthService()
	users.addUser(t, authService, models.RoleEditor, "СотрудникРед", "надёжный-пароль")
	attempts := &fakeAuthAttemptStore{}
	h := NewAuthHandler(users, authService, attempts, "test-secret", false)

	rec := httptest.NewRecorder()
	h.ReaderLogin(rec, readerLoginRequest("СотрудникРед", "надёжный-пароль"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("сотрудник не должен входить читательской дверью, получено %d: %s",
			rec.Code, rec.Body.String())
	}
}

// --- Скользящий срок сессии: GET /api/auth/me ---

func TestMeRefreshesAgedReaderToken(t *testing.T) {
	users := newFakeAuthUserStore()
	handler := NewAuthHandler(users, newTestAuthService(), &fakeAuthAttemptStore{}, "test-secret", false)

	// Claims читателя с IssuedAt восьмидневной давности. Продление теперь
	// проверяет учётку в базе (findByID), поэтому строка должна там быть —
	// иначе это TestMeStopsRenewalForDeletedReader ниже.
	nickname := "AgedReader"
	users.created = append(users.created, &models.User{ID: 42, Role: models.RoleReader, Nickname: &nickname})
	claims := &auth.Claims{
		UserID:   42,
		Email:    "reader@example.com",
		Role:     models.RoleReader,
		Nickname: nickname,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt: jwt.NewNumericDate(time.Now().Add(-8 * 24 * time.Hour)),
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.Me(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ожидался 200, получено %d: %s", rec.Code, rec.Body.String())
	}

	var response MeResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}

	// В ответе должен быть новый токен
	if response.Token == "" {
		t.Fatal("поле token пусто — продление не выдано")
	}

	// Должны быть данные пользователя
	if response.ID != 42 {
		t.Errorf("ID = %d, ожидалось 42", response.ID)
	}
	if response.Email != "reader@example.com" {
		t.Errorf("Email = %s, ожидалось reader@example.com", response.Email)
	}
	if response.Role != models.RoleReader {
		t.Errorf("Role = %s, ожидалась %s", response.Role, models.RoleReader)
	}
	if response.Nickname == nil || *response.Nickname != nickname {
		t.Errorf("Nickname = %v, ожидался %s", response.Nickname, nickname)
	}
}

// TestMeStopsRenewalForDeletedReader — БЛОКЕР рецензии: продление раньше
// подписывало новый 90-суточный токен прямо из claims, не заглядывая в базу.
// Читатель, удаливший свою учётку (или удалённый администратором) неделю
// назад, получал на этом самом запросе свежий токен на несуществующего
// пользователя — /collections/mine отвечал 200 пустым списком, а запись
// падала 500-й на нарушении внешнего ключа. Теперь продление сверяется с
// GetByID, и на отсутствующем пользователе отвечает 401, а не продлевает.
func TestMeStopsRenewalForDeletedReader(t *testing.T) {
	users := newFakeAuthUserStore()
	handler := NewAuthHandler(users, newTestAuthService(), &fakeAuthAttemptStore{}, "test-secret", false)

	// Пользователя с этим ID в хранилище нет вовсе — учётка удалена.
	claims := &auth.Claims{
		UserID:   99,
		Email:    "gone@example.com",
		Role:     models.RoleReader,
		Nickname: "УдалённыйЧитатель",
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt: jwt.NewNumericDate(time.Now().Add(-8 * 24 * time.Hour)),
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.Me(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ожидался 401 для удалённого читателя, получено %d: %s", rec.Code, rec.Body.String())
	}
	if msg := decodeErrorMessage(t, rec.Body.Bytes()); msg == "" {
		t.Fatal("в теле ошибки нет message")
	}
}

func TestMeDoesNotRefreshFreshReaderToken(t *testing.T) {
	handler := createTestAuthHandler()

	// Claims читателя, выданный час назад
	nickname := "FreshReader"
	claims := &auth.Claims{
		UserID:   43,
		Email:    "fresh@example.com",
		Role:     models.RoleReader,
		Nickname: nickname,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.Me(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ожидался 200, получено %d: %s", rec.Code, rec.Body.String())
	}

	var response MeResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}

	// Поля token не должно быть или оно должно быть пусто
	if response.Token != "" {
		t.Fatalf("поле token не должно быть заполнено при свежем токене, получено %q", response.Token)
	}

	// Должны быть данные пользователя
	if response.ID != 43 {
		t.Errorf("ID = %d, ожидалось 43", response.ID)
	}
}

func TestMeNeverRefreshesStaffToken(t *testing.T) {
	handler := createTestAuthHandler()

	// Claims редактора с IssuedAt восьмидневной давности
	// Сотруднику продление не даётся никогда, чтобы суточная сессия не стала вечной
	claims := &auth.Claims{
		UserID: 44,
		Email:  "staff@example.com",
		Role:   models.RoleEditor,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt: jwt.NewNumericDate(time.Now().Add(-8 * 24 * time.Hour)),
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	ctx := context.WithValue(req.Context(), middleware.UserContextKey, claims)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.Me(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ожидался 200, получено %d: %s", rec.Code, rec.Body.String())
	}

	var response MeResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}

	// Поля token не должно быть или оно должно быть пусто
	if response.Token != "" {
		t.Fatalf("сотруднику не даётся продление, token должен быть пуст, получено %q", response.Token)
	}

	// Должны быть данные пользователя
	if response.ID != 44 {
		t.Errorf("ID = %d, ожидалось 44", response.ID)
	}
	if response.Role != models.RoleEditor {
		t.Errorf("Role = %s, ожидалась %s", response.Role, models.RoleEditor)
	}
}

// Дверь ухода закрыта сотруднику: его учётку заводит и снимает
// администратор через /users, где стоит охрана последнего администратора.
// Обойти её этой дверью нельзя — обработчик обязан отказать честным 403 ещё
// до обращения к репозиторию.
func TestDeleteMeRefusesStaff(t *testing.T) {
	h := createTestAuthHandler()

	req := httptest.NewRequest(http.MethodDelete, "/api/auth/me", nil)
	rec := httptest.NewRecorder()
	withClaims(editorClaims(), h.DeleteMe)(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("сотрудник снял себя читательской дверью: код %d (%s)", rec.Code, rec.Body.String())
	}
}

// Без токена вовсе — честный 401, а не паника на nil claims.
func TestDeleteMeRefusesAnonymous(t *testing.T) {
	h := createTestAuthHandler()

	req := httptest.NewRequest(http.MethodDelete, "/api/auth/me", nil)
	rec := httptest.NewRecorder()
	h.DeleteMe(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("анонимный запрос ответил %d, ждали 401", rec.Code)
	}
}

// Читатель, снявший себя, получает 204 и учётка действительно уходит со
// склада — это и есть весь контракт DeleteMe: осиротение труда проверяется
// отдельно, в репозитории (TestDeleteReaderOrphansButKeepsWork).
func TestDeleteMeRemovesOwnAccount(t *testing.T) {
	users := newFakeAuthUserStore()
	nick := "уходящий"
	reader := &models.User{ID: 5, Role: models.RoleReader, Nickname: &nick}
	users.created = append(users.created, reader)
	users.byNicknameKey[models.NormalizeNickname(nick)] = reader

	h := NewAuthHandler(users, newTestAuthService(), &fakeAuthAttemptStore{}, "test-secret", false)

	req := httptest.NewRequest(http.MethodDelete, "/api/auth/me", nil)
	rec := httptest.NewRecorder()
	withClaims(readerClaims(5, nick), h.DeleteMe)(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("код %d (%s)", rec.Code, rec.Body.String())
	}
	if !users.deleted[5] {
		t.Fatal("учётная запись не удалена")
	}
	if _, err := users.GetByID(context.Background(), 5); err == nil {
		t.Fatal("строка читателя всё ещё на складе после ухода")
	}
}
