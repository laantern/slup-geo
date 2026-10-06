// Package schema хранит SQL-скрипты схемы geo (встроены в бинарник).
package schema

import (
	"embed"
	"fmt"
)

//go:embed sql/*.sql
var files embed.FS

// Extensions — расширения БД (применяются до импорта).
func Extensions() (string, error) {
	return read("sql/00-extensions.sql")
}

// Views — представления и матвью (применяются после импорта: planet-таблицы пересоздаются).
func Views() ([]string, error) {
	views, err := read("sql/01-views.sql")
	if err != nil {
		return nil, err
	}
	names, err := read("sql/02-names.sql")
	if err != nil {
		return nil, err
	}
	return []string{views, names}, nil
}

func read(name string) (string, error) {
	content, err := files.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("чтение встроенного скрипта %s: %w", name, err)
	}
	return string(content), nil
}
