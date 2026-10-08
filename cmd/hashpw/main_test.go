package main

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Хэш обязан приниматься той же проверкой, что делает вход (bcrypt), и не
// включать хвостовой перевод строки: echo admin | hashpw — частый способ.
func TestHashAcceptsPasswordWithoutTrailingNewline(t *testing.T) {
	h, err := hash(strings.NewReader("секрет\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(h), []byte("секрет")); err != nil {
		t.Fatalf("хэш не принимает пароль: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(h), []byte("секрет\n")) == nil {
		t.Fatal("хвостовой перевод строки попал в пароль")
	}
}

func TestHashRefusesEmptyPassword(t *testing.T) {
	if _, err := hash(strings.NewReader("\n")); err == nil {
		t.Fatal("пустой пароль принят")
	}
}
