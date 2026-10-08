package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
)

// Предел попыток входа — 10 в час по паре (отметка адреса, ник). Перебор
// имён безвреден (ники публичны по построению), а подбор пароля — нет: чужой
// ник со слабым паролем это публикация от чужого имени на нашем домене.
const (
	readerLoginAttemptsPerHour = 10
	readerSignupsPerDay        = 3
	readerMinPasswordRunes     = 8
	// readerMaxPasswordBytes — предел bcrypt: он молча обрезает пароль длиннее
	// 72 байт при хэшировании, а bcrypt.GenerateFromPassword сверх этого
	// вовсе отказывает ошибкой (см. golang.org/x/crypto/bcrypt). Без проверки
	// здесь длинный пароль падал 500-й на «Не удалось завести читателя» — в
	// точке невозврата: ник уже занят этим запросом, а восстановления учётки
	// нет. Байты, а не знаки: кириллица идёт двумя байтами на букву, поэтому
	// предел в рунах пропустил бы пароль короче 72 знаков, но длиннее 72 байт.
	readerMaxPasswordBytes = 72
)

// authUserStore — срез репозитория пользователей, которым пользуется
// AuthHandler. Объявлен на стороне потребителя (как userStore в
// user_handler.go), чтобы обе двери — сотрудническую и читательскую —
// прогонять без базы. *repository.UserRepository удовлетворяет ему как есть.
type authUserStore interface {
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	GetByNickname(ctx context.Context, nickname string) (*models.User, error)
	GetByID(ctx context.Context, id int64) (*models.User, error)
	CreateReaderWithinLimit(ctx context.Context, user *models.User, limit int, since time.Time) (bool, error)
	// DeleteReader — уход читателя (DeleteMe). Условие по роли стоит и здесь,
	// в самом запросе репозитория, а не только в обработчике — см. её докстроку.
	// Она же отставляет ник ушедшего одной транзакцией с удалением строки.
	DeleteReader(ctx context.Context, id int64) error
	// IsNicknameRetired — числится ли имя за ушедшим читателем. Читательская
	// дверь такое имя не заводит: под ним в читальне остались тексты.
	IsNicknameRetired(ctx context.Context, nickname string) (bool, error)
}

// authAttemptStore — срез репозитория попыток входа. Ключ — пара (отметка
// адреса, ник), см. docstring ReserveAttempt в internal/repository.
// *repository.AuthAttemptRepository удовлетворяет ему как есть.
type authAttemptStore interface {
	ReserveAttempt(ctx context.Context, ipHash, nickname string, limit int, since time.Time) (bool, error)
	Clear(ctx context.Context, ipHash, nickname string) error
}

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	userRepo    authUserStore
	authService *auth.Service
	attempts    authAttemptStore
	jwtSecret   string
	trustProxy  bool
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(
	userRepo authUserStore,
	authService *auth.Service,
	attempts authAttemptStore,
	jwtSecret string,
	trustProxy bool,
) *AuthHandler {
	return &AuthHandler{
		userRepo:    userRepo,
		authService: authService,
		attempts:    attempts,
		jwtSecret:   jwtSecret,
		trustProxy:  trustProxy,
	}
}

// LoginRequest represents a login request
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// ReaderLoginRequest — тело читательской двери.
type ReaderLoginRequest struct {
	Nickname string `json:"nickname"`
	Password string `json:"password"`
}

// AuthResponse represents an authentication response
type AuthResponse struct {
	User         UserResponse `json:"user"`
	Token        string       `json:"token"`
	RefreshToken string       `json:"refresh_token"`
}

// UserResponse represents a user in API responses
type UserResponse struct {
	ID    int64           `json:"id"`
	Email string          `json:"email"`
	Role  models.UserRole `json:"role"`
	// Nickname есть только у читателя; у сотрудника — nil.
	Nickname *string `json:"nickname,omitempty"`
}

// MeResponse — ответ /auth/me. Token непустой только тогда, когда
// читательскую сессию продлили.
type MeResponse struct {
	UserResponse
	Token string `json:"token,omitempty"`
}

func nicknamePtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Login handles user login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get user by email
	ctx := context.Background()
	user, err := h.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Verify password
	if !h.authService.VerifyPassword(req.Password, user.PasswordHash) {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Generate tokens
	token, err := h.authService.GenerateToken(user)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	refreshToken, err := h.authService.GenerateRefreshToken(user)
	if err != nil {
		http.Error(w, "Failed to generate refresh token", http.StatusInternalServerError)
		return
	}

	response := AuthResponse{
		User: UserResponse{
			ID:       user.ID,
			Email:    user.Email,
			Role:     user.Role,
			Nickname: user.Nickname,
		},
		Token:        token,
		RefreshToken: refreshToken,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ReaderLogin — вход, равный регистрации: есть такой ник и пароль сошёлся —
// вход; нет такого — заводится.
//
// Дверь отдельная от сотруднической намеренно. Жест «ввёл — завёлся» опасен и
// не должен стоять в одном поле с сотрудническим входом, у которого опечатка в
// почте обязана кончаться отказом, а не новым пользователем.
func (h *AuthHandler) ReaderLogin(w http.ResponseWriter, r *http.Request) {
	var req ReaderLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Не удалось разобрать запрос")
		return
	}

	if err := models.ValidateNickname(req.Nickname); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if utf8.RuneCountInString(req.Password) < readerMinPasswordRunes {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("Пароль короче %d знаков", readerMinPasswordRunes))
		return
	}
	if len(req.Password) > readerMaxPasswordBytes {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("Пароль длиннее %d байт (кириллица и другие не-латинские буквы "+
				"занимают по два байта на знак — сократите пароль)", readerMaxPasswordBytes))
		return
	}

	ctx := context.Background()
	nickname := strings.TrimSpace(req.Nickname)
	ipHash := hashIP(clientIP(r, h.trustProxy), h.jwtSecret)

	// Ключ предела — пара (адрес, ник), а не голый адрес: см. докблок
	// ReserveAttempt в internal/repository/auth_attempt_repository.go.
	allowed, err := h.attempts.ReserveAttempt(ctx, ipHash, nickname,
		readerLoginAttemptsPerHour, time.Now().Add(-time.Hour))
	if err != nil {
		log.Printf("reserve auth attempt: %v", err)
		writeError(w, http.StatusInternalServerError, "Не удалось войти")
		return
	}
	if !allowed {
		writeError(w, http.StatusTooManyRequests,
			"Слишком много попыток входа. Попробуйте позже.")
		return
	}

	user, err := h.userRepo.GetByNickname(ctx, nickname)
	if err == nil {
		// Читатель есть. Отвечаем честно: ники публичны по построению, и
		// уклончивый ответ оставил бы человека гадать, что ему делать.
		if !h.authService.VerifyPassword(req.Password, user.PasswordHash) {
			writeError(w, http.StatusUnauthorized,
				"Такой читатель уже есть, пароль не подошёл")
			return
		}
		if user.Role != models.RoleReader {
			// Сотрудник входит своей дверью. Иначе читательский срок сессии
			// достался бы сотруднику.
			writeError(w, http.StatusUnauthorized,
				"Такой читатель уже есть, пароль не подошёл")
			return
		}
		if err := h.attempts.Clear(ctx, ipHash, nickname); err != nil {
			log.Printf("clear auth attempts: %v", err)
		}
		h.respondWithToken(w, user)
		return
	}

	// Такого ника нет. Но «нет строки» ещё не значит «имя свободно»: ушедший
	// читатель уносит строку, а подпись под тем, что он оставил, — нет
	// (documents/collections.author_nickname держат СНИМОК ника). Заведи
	// такое имя заново — и чужой опубликованный разбор начнёт печатать
	// «Собрал читатель ‹ник›» про живого человека, который этого не писал.
	// Тот же довод, которым ValidateNickname запрещает гомоглифы.
	retired, err := h.userRepo.IsNicknameRetired(ctx, nickname)
	if err != nil {
		log.Printf("check retired nickname: %v", err)
		writeError(w, http.StatusInternalServerError, "Не удалось войти")
		return
	}
	if retired {
		// 409, а не 401: отказ обязан быть отличим от «пароль не подошёл» —
		// подбирать тут нечего, имя не освободится никогда, и человеку нужно
		// выбрать другое, а не вспоминать пароль.
		writeError(w, http.StatusConflict,
			"Под этим именем в читальне остались тексты — их собрал читатель, "+
				"который ушёл. Занять это имя нельзя, выберите другое.")
		return
	}

	hash, err := h.authService.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось завести читателя")
		return
	}
	fresh := &models.User{
		Role:         models.RoleReader,
		Nickname:     &nickname,
		PasswordHash: hash,
		SignupIPHash: ipHash,
	}
	created, err := h.userRepo.CreateReaderWithinLimit(ctx, fresh,
		readerSignupsPerDay, time.Now().Add(-24*time.Hour))
	if err != nil {
		// Гонка двух одинаковых ников кончается здесь: уникальность держит
		// индекс, и второй получает отказ, а не молчаливого двойника.
		log.Printf("create reader: %v", err)
		writeError(w, http.StatusConflict, "Это имя только что заняли. Выберите другое.")
		return
	}
	if !created {
		writeError(w, http.StatusTooManyRequests,
			"С этого адреса сегодня уже заводили читателей. Попробуйте завтра.")
		return
	}
	if err := h.attempts.Clear(ctx, ipHash, nickname); err != nil {
		log.Printf("clear auth attempts: %v", err)
	}
	h.respondWithToken(w, fresh)
}

// respondWithToken собирает тот же ответ, что и сотрудническая дверь.
func (h *AuthHandler) respondWithToken(w http.ResponseWriter, user *models.User) {
	token, err := h.authService.GenerateToken(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось выдать токен")
		return
	}
	refresh, err := h.authService.GenerateRefreshToken(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось выдать токен")
		return
	}
	writeJSONStatus(w, http.StatusOK, AuthResponse{
		User: UserResponse{
			ID:       user.ID,
			Email:    user.Email,
			Nickname: user.Nickname,
			Role:     user.Role,
		},
		Token:        token,
		RefreshToken: refresh,
	})
}

// Me returns the current authenticated user
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	// Ключ контекста берём у middleware, а не строкой: там он объявлен
	// собственным типом (contextKey), а ключи контекста в Go сравниваются
	// вместе с типом. Литерал "user" не совпадал с contextKey("user"), поэтому
	// эндпоинт отвечал 401 на любой валидный токен.
	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	response := MeResponse{UserResponse: UserResponse{
		ID:       claims.UserID,
		Email:    claims.Email,
		Nickname: nicknamePtr(claims.Nickname),
		Role:     claims.Role,
	}}

	// Скользящий срок делается здесь, а не на каждом запросе: перевыпуск
	// токена в middleware стоил бы подписи на каждом обращении. Неделя —
	// достаточно редко, чтобы не мешать, и достаточно часто, чтобы
	// девяностосуточный срок не истёк у живого читателя.
	const refreshAfter = 7 * 24 * time.Hour
	if claims.Role == models.RoleReader && claims.IssuedAt != nil &&
		time.Since(claims.IssuedAt.Time) > refreshAfter {
		// БЛОКЕР рецензии: токен раньше перевыпускался прямо из claims, без
		// обращения к базе. Читатель, чья учётка на этой неделе удалена
		// (свою собственную удаляют сами — см. Delete ниже), получал
		// свежий 90-суточный токен на несуществующего пользователя:
		// /collections/mine отвечал 200 пустым списком, а любая попытка
		// записи упиралась в 500 по нарушению внешнего ключа. Раз в неделю
		// на читателя — цена одного GetByID, не на каждый запрос.
		if _, err := h.userRepo.GetByID(r.Context(), claims.UserID); err != nil {
			writeError(w, http.StatusUnauthorized, "Учётная запись не найдена")
			return
		}
		user := &models.User{
			ID:       claims.UserID,
			Email:    claims.Email,
			Role:     claims.Role,
			Nickname: nicknamePtr(claims.Nickname),
		}
		if token, err := h.authService.GenerateToken(user); err == nil {
			response.Token = token
		} else {
			log.Printf("refresh reader token: %v", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// DeleteMe — уход читателя из читальни. Необратимо и без восстановления:
// почты у читателя нет принципиально (решение 08), поэтому вернуть учётку
// нечем, а новый вход с тем же ником заведёт ДРУГОГО человека — ник
// освобождается вместе со строкой.
//
// Сотруднику эта дверь закрыта: его учётку заводит и снимает администратор
// через /users, где и стоит охрана последнего администратора. Проверка роли
// продублирована в DeleteReader, в самом SQL, — эта здесь ради честного 403
// вместо «не найдено».
//
// Выданный токен удаление НЕ прекращает: AuthMiddleware проверяет подпись и
// срок, а не наличие строки, и читательская сессия живёт до 90 суток.
// Практически это значит, что ушедший остаётся «вошедшим» до тех пор, пока
// клиент не выбросит токен (это делает экран ухода) или пока GET /auth/me не
// ответит 401, не найдя строки, — а это происходит не сразу: перевыпуск
// токена (и с ним проверка существования строки) в Me срабатывает только раз
// в неделю (см. refreshAfter выше), так что токен младше недели переживает
// удаление молча и отвечает на /auth/me 200 по данным из самого токена.
// Обещать «сессия прекращена» эта кнопка не может.
func (h *AuthHandler) DeleteMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "Нужно записаться в читальню")
		return
	}
	if claims.Role != models.RoleReader {
		writeError(w, http.StatusForbidden,
			"Учётную запись сотрудника снимает администратор")
		return
	}
	if err := h.userRepo.DeleteReader(context.Background(), claims.UserID); err != nil {
		// Токен переживает удаление (см. абзац выше), поэтому второй запрос
		// достижим обычным двойным нажатием — и отвечать на него сбоем
		// сервера нечестно: ничего не сломалось, учётной записи просто уже
		// нет. Тот же приём разведения «нет строки» от «беда в хранилище»,
		// что и в обработчиках разбора (storageNotFound).
		if storageNotFound(err) {
			writeError(w, http.StatusNotFound, "Учётной записи уже нет")
			return
		}
		log.Printf("delete reader: %v", err)
		writeError(w, http.StatusInternalServerError, "Не удалось удалить учётную запись")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
