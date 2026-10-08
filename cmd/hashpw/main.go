// Command hashpw печатает bcrypt-хэш пароля со stdin — той же стоимости, что
// ставит auth.Service.HashPassword. Нужен scripts/adopt-prod-db.sh: после
// приёма боевого дампа у локального администратора боевой пароль, а
// seedAdminUser существующего пользователя не трогает — хэш пароля из
// локального .env ставится прямо в базе. bcrypt в bash считать нечем.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func hash(r io.Reader) (string, error) {
	raw, err := io.ReadAll(bufio.NewReader(r))
	if err != nil {
		return "", err
	}
	pw := strings.TrimSuffix(string(raw), "\n")
	if pw == "" {
		return "", errors.New("пустой пароль")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

func main() {
	h, err := hash(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hashpw:", err)
		os.Exit(1)
	}
	fmt.Println(h)
}
