package auth

import (
	"errors"
	"log"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"proofreader/internal/config"
	"proofreader/internal/models"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token has expired")
)

// Claims represents JWT claims
type Claims struct {
	UserID int64           `json:"user_id"`
	Email  string          `json:"email"`
	Role   models.UserRole `json:"role"`
	// Nickname нужен фронту в шапке и обработчикам — для подписи подборки.
	Nickname string `json:"nickname,omitempty"`
	jwt.RegisteredClaims
}

// Service handles authentication operations
type Service struct {
	cfg *config.JWTConfig
	// readerExpiration — вычисленный в конструкторе срок читательского
	// токена. Отдельное поле, а не cfg.ReaderExpiration, потому что
	// конструктор не вправе молча переписывать объект вызывающего: main.go
	// вправе рассчитывать, что *config.JWTConfig после загрузки неизменен.
	readerExpiration time.Duration
}

// NewService creates a new auth service.
//
// Неположительный ReaderExpiration откатывается на Expiration с
// предупреждением в лог, а не остаётся как есть и не роняет старт. Молчаливый
// откат исключён нарочно: getEnvAsDuration (internal/config) подставляет
// умолчание только при пустой или неразбираемой строке — явный
// JWT_READER_EXPIRATION=0 или собранный руками конфиг (как в тестах) проходят
// её насквозь и без этой проверки давали бы token с ExpiresAt в прошлом:
// /auth/reader отвечал бы 200 с мёртвым токеном, следующий запрос — 401, и
// читатель не мог бы войти вовсе, без единой строки в логе. Фатальный отказ
// при загрузке конфига здесь неуместен: срок читательской сессии не повод
// не поднимать читальню целиком, а тем более для сотрудников, которых
// ReaderExpiration не касается вовсе.
//
// Расчёт откладывается в readerExpiration, а cfg остаётся как получен —
// конструктору не пристало на месте править объект, переданный по указателю
// вызывающим кодом.
func NewService(cfg *config.JWTConfig) *Service {
	readerExpiration := cfg.ReaderExpiration
	if readerExpiration <= 0 {
		log.Printf("предупреждение: JWT_READER_EXPIRATION <= 0, читательские токены используют общий срок (%s)",
			cfg.Expiration)
		readerExpiration = cfg.Expiration
	}
	return &Service{cfg: cfg, readerExpiration: readerExpiration}
}

// HashPassword hashes a password using bcrypt
func (s *Service) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword verifies a password against a hash
func (s *Service) VerifyPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// GenerateToken generates a new JWT token for a user
func (s *Service) GenerateToken(user *models.User) (string, error) {
	claims := &Claims{
		UserID:   user.ID,
		Email:    user.Email,
		Role:     user.Role,
		Nickname: nicknameOf(user),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.tokenTTL(user.Role))),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.Secret))
}

// GenerateRefreshToken generates a refresh token
func (s *Service) GenerateRefreshToken(user *models.User) (string, error) {
	claims := &Claims{
		UserID:   user.ID,
		Email:    user.Email,
		Role:     user.Role,
		Nickname: nicknameOf(user),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.cfg.RefreshExpiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.Secret))
}

// ValidateToken validates a JWT token and returns the claims
func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(s.cfg.Secret), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	// Check if token is expired
	if claims.ExpiresAt.Before(time.Now()) {
		return nil, ErrExpiredToken
	}

	return claims, nil
}

// tokenTTL выбирает срок жизни токена по роли. Читателю — свой, длинный;
// сотруднику — прежние сутки, которые трогать нельзя.
func (s *Service) tokenTTL(role models.UserRole) time.Duration {
	if role == models.RoleReader {
		return s.readerExpiration
	}
	return s.cfg.Expiration
}

// nicknameOf разыменовывает Nickname, не паникуя на сотруднике, у которого
// поле nil.
func nicknameOf(user *models.User) string {
	if user.Nickname == nil {
		return ""
	}
	return *user.Nickname
}
