package auth

import (
	"testing"
	"time"

	"proofreader/internal/config"
	"proofreader/internal/models"
)

func TestNewService(t *testing.T) {
	cfg := &config.JWTConfig{
		Secret:            "test-secret",
		Expiration:        time.Hour,
		RefreshExpiration: 24 * time.Hour,
	}

	service := NewService(cfg)
	if service == nil {
		t.Fatal("Expected service to be created")
	}
	if service.cfg != cfg {
		t.Error("Expected config to be set")
	}
}

func TestHashPassword(t *testing.T) {
	cfg := &config.JWTConfig{
		Secret:            "test-secret",
		Expiration:        time.Hour,
		RefreshExpiration: 24 * time.Hour,
	}
	service := NewService(cfg)

	tests := []struct {
		name     string
		password string
	}{
		{"simple password", "password123"},
		{"complex password", "P@ssw0rd!#$%^&*()"},
		{"unicode password", "пароль123"},
		{"empty password", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := service.HashPassword(tt.password)
			if err != nil {
				t.Fatalf("HashPassword failed: %v", err)
			}
			if hash == "" {
				t.Error("Expected non-empty hash")
			}
			if hash == tt.password {
				t.Error("Hash should not equal plain password")
			}
		})
	}
}

func TestVerifyPassword(t *testing.T) {
	cfg := &config.JWTConfig{
		Secret:            "test-secret",
		Expiration:        time.Hour,
		RefreshExpiration: 24 * time.Hour,
	}
	service := NewService(cfg)

	password := "testPassword123"
	hash, err := service.HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	tests := []struct {
		name     string
		password string
		hash     string
		expected bool
	}{
		{"correct password", password, hash, true},
		{"wrong password", "wrongPassword", hash, false},
		{"empty password", "", hash, false},
		{"case sensitive", "TESTPASSWORD123", hash, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.VerifyPassword(tt.password, tt.hash)
			if result != tt.expected {
				t.Errorf("VerifyPassword(%q) = %v, want %v", tt.password, result, tt.expected)
			}
		})
	}
}

func TestGenerateToken(t *testing.T) {
	cfg := &config.JWTConfig{
		Secret:            "test-secret-key-for-testing",
		Expiration:        time.Hour,
		RefreshExpiration: 24 * time.Hour,
	}
	service := NewService(cfg)

	user := &models.User{
		ID:    1,
		Email: "test@example.com",
		Role:  models.RoleEditor,
	}

	token, err := service.GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	if token == "" {
		t.Error("Expected non-empty token")
	}
}

func TestGenerateRefreshToken(t *testing.T) {
	cfg := &config.JWTConfig{
		Secret:            "test-secret-key-for-testing",
		Expiration:        time.Hour,
		RefreshExpiration: 24 * time.Hour,
	}
	service := NewService(cfg)

	user := &models.User{
		ID:    1,
		Email: "test@example.com",
		Role:  models.RoleEditor,
	}

	token, err := service.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("GenerateRefreshToken failed: %v", err)
	}
	if token == "" {
		t.Error("Expected non-empty refresh token")
	}
}

func TestValidateToken(t *testing.T) {
	cfg := &config.JWTConfig{
		Secret:            "test-secret-key-for-testing",
		Expiration:        time.Hour,
		RefreshExpiration: 24 * time.Hour,
	}
	service := NewService(cfg)

	user := &models.User{
		ID:    123,
		Email: "user@test.com",
		Role:  models.RoleAdministrator,
	}

	t.Run("valid token", func(t *testing.T) {
		token, err := service.GenerateToken(user)
		if err != nil {
			t.Fatalf("Failed to generate token: %v", err)
		}

		claims, err := service.ValidateToken(token)
		if err != nil {
			t.Fatalf("ValidateToken failed: %v", err)
		}
		if claims.UserID != user.ID {
			t.Errorf("UserID = %d, want %d", claims.UserID, user.ID)
		}
		if claims.Email != user.Email {
			t.Errorf("Email = %s, want %s", claims.Email, user.Email)
		}
		if claims.Role != user.Role {
			t.Errorf("Role = %s, want %s", claims.Role, user.Role)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		_, err := service.ValidateToken("invalid.token.here")
		if err == nil {
			t.Error("Expected error for invalid token")
		}
	})

	t.Run("wrong secret", func(t *testing.T) {
		otherCfg := &config.JWTConfig{
			Secret:            "different-secret",
			Expiration:        time.Hour,
			RefreshExpiration: 24 * time.Hour,
		}
		otherService := NewService(otherCfg)

		token, _ := otherService.GenerateToken(user)
		_, err := service.ValidateToken(token)
		if err == nil {
			t.Error("Expected error for token with wrong secret")
		}
	})

	t.Run("expired token", func(t *testing.T) {
		expiredCfg := &config.JWTConfig{
			Secret:            "test-secret-key-for-testing",
			Expiration:        -time.Hour,
			RefreshExpiration: 24 * time.Hour,
		}
		expiredService := NewService(expiredCfg)

		token, _ := expiredService.GenerateToken(user)
		_, err := service.ValidateToken(token)
		if err == nil {
			t.Error("Expected error for expired token")
		}
	})
}

func TestDifferentRoles(t *testing.T) {
	cfg := &config.JWTConfig{
		Secret:            "test-secret",
		Expiration:        time.Hour,
		RefreshExpiration: 24 * time.Hour,
	}
	service := NewService(cfg)

	roles := []models.UserRole{
		models.RoleAdministrator,
		models.RoleEditor,
	}

	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			user := &models.User{
				ID:    1,
				Email: "test@example.com",
				Role:  role,
			}

			token, err := service.GenerateToken(user)
			if err != nil {
				t.Fatalf("Failed to generate token: %v", err)
			}

			claims, err := service.ValidateToken(token)
			if err != nil {
				t.Fatalf("Failed to validate token: %v", err)
			}

			if claims.Role != role {
				t.Errorf("Role = %s, want %s", claims.Role, role)
			}
		})
	}
}

func TestGenerateTokenUsesReaderExpirationForReaders(t *testing.T) {
	cfg := &config.JWTConfig{
		Secret:           "тест",
		Expiration:       24 * time.Hour,
		ReaderExpiration: 90 * 24 * time.Hour,
	}
	svc := NewService(cfg)

	reader := &models.User{ID: 7, Role: models.RoleReader}
	token, err := svc.GenerateToken(reader)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}

	left := time.Until(claims.ExpiresAt.Time)
	if left < 80*24*time.Hour {
		t.Errorf("читательскому токену осталось %v, ждали около 90 суток", left)
	}

	staff := &models.User{ID: 1, Role: models.RoleEditor}
	staffToken, _ := svc.GenerateToken(staff)
	staffClaims, _ := svc.ValidateToken(staffToken)
	if time.Until(staffClaims.ExpiresAt.Time) > 25*time.Hour {
		t.Error("сотруднику выдан читательский срок — суточная сессия молча стала вечной")
	}
}
