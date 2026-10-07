// Package schema хранит SQL-скрипты схемы geo (встроены в бинарник).
package schema

import (
	"embed"
	"fmt"
	"strings"
)

// Version — версия схемы данных (geo.meta.schema_version). Меняется при несовместимых
// изменениях SQL-представлений: serve сравнивает её со своей и предупреждает о рассинхроне.
const Version = 1

//go:embed sql/*.sql
var files embed.FS

// Extensions — расширения БД (применяются до импорта).
func Extensions() (string, error) {
	return read("sql/00-extensions.sql")
}

// Views — представления для канонических таблиц planet_osm_* (тесты, инструменты,
// внешние сценарии). В штатном обновлении не используются: см. ViewsStaging.
func Views() ([]string, error) {
	return render("", "")
}

// ViewsStaging — те же представления, но по staging-таблицам planet_osm_next_*
// и с суффиксом «_next» у матвью (geo.zones_next и т.д.). Собираются ДО замены рабочих
// таблиц, вне транзакции: читатели API не блокируются. Финальный swap — короткая
// метаданная транзакция (DROP старых + RENAME staging в канонические имена).
func ViewsStaging() ([]string, error) {
	return render("_next", "_next")
}

// render подставляет в шаблоны SQL суффиксы: {{pfx}} — к именам planet_osm_*-таблиц,
// {{sfx}} — к именам матвью и их индексов.
func render(planetSuffix, viewSuffix string) ([]string, error) {
	views, err := read("sql/01-views.sql")
	if err != nil {
		return nil, err
	}
	names, err := read("sql/02-names.sql")
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, 2)
	for _, script := range []string{views, names} {
		script = strings.ReplaceAll(script, "{{pfx}}", planetSuffix)
		script = strings.ReplaceAll(script, "{{sfx}}", viewSuffix)
		out = append(out, script)
	}
	return out, nil
}

func read(name string) (string, error) {
	content, err := files.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("чтение встроенного скрипта %s: %w", name, err)
	}
	return string(content), nil
}
