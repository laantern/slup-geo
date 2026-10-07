package config

import (
	"strings"
	"testing"
)

func TestEnvIntStrict(t *testing.T) {
	t.Setenv("TEST_INT", "abc")
	if _, err := envInt("TEST_INT", 5, 0, 10); err == nil {
		t.Fatal("мусор должен приводить к ошибке конфигурации")
	}

	t.Setenv("TEST_INT", "-5")
	if _, err := envInt("TEST_INT", 5, 0, 10); err == nil {
		t.Fatal("значение вне диапазона должно приводить к ошибке")
	}

	t.Setenv("TEST_INT", "99")
	if _, err := envInt("TEST_INT", 5, 0, 10); err == nil {
		t.Fatal("значение выше максимума должно приводить к ошибке")
	}

	t.Setenv("TEST_INT", "")
	if v, err := envInt("TEST_INT", 5, 0, 10); err != nil || v != 5 {
		t.Fatalf("пустое значение = %d, %v; ожидался дефолт 5", v, err)
	}

	t.Setenv("TEST_INT", "7")
	if v, err := envInt("TEST_INT", 5, 0, 10); err != nil || v != 7 {
		t.Fatalf("значение = %d, %v; ожидалось 7", v, err)
	}
}

func TestDatabaseDSNRequiresPassword(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PGPASSWORD", "")
	if _, err := databaseDSN(); err == nil {
		t.Fatal("без PGPASSWORD должна быть ошибка (дефолтного пароля нет)")
	}

	t.Setenv("PGPASSWORD", "secret")
	dsn, err := databaseDSN()
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if !strings.Contains(dsn, "secret") {
		t.Fatalf("DSN не содержит пароль: %s", dsn)
	}

	t.Setenv("DATABASE_URL", "postgres://u:p@h/db")
	if dsn, err := databaseDSN(); err != nil || dsn != "postgres://u:p@h/db" {
		t.Fatalf("DATABASE_URL должен иметь приоритет, получено %q, %v", dsn, err)
	}
}
