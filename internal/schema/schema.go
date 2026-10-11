// Package schema хранит SQL-скрипты схемы geo (встроены в бинарник).
package schema

import (
	"embed"
	"fmt"
	"strconv"
	"strings"
)

// Version — версия схемы данных (geo.meta.schema_version). Меняется при несовместимых
// изменениях SQL-представлений: serve сравнивает её со своей и предупреждает о рассинхроне.
// v2: в нормализации имён (geo.names) сохраняются белорусские і/ў и украинские ї/є/ґ.
const Version = 2

//go:embed sql/*.sql
var files embed.FS

// Extensions — расширения БД (применяются до импорта).
func Extensions() (string, error) {
	return read("sql/00-extensions.sql")
}

// Views — представления для канонических таблиц planet_osm_* (тесты, инструменты,
// внешние сценарии) вместе с записью версии схемы. В штатном обновлении не используются:
// см. ViewsStaging и Meta.
func Views() ([]string, error) {
	views, err := render("", "")
	if err != nil {
		return nil, err
	}
	meta, err := Meta()
	if err != nil {
		return nil, err
	}
	return append(views, meta), nil
}

// Meta — запись текущей версии схемы в geo.meta. В штатном обновлении выполняется после
// успешной замены таблиц: версия в БД всегда описывает рабочие данные.
func Meta() (string, error) {
	script, err := read("sql/03-meta.sql")
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(script, "{{version}}", strconv.Itoa(Version)), nil
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
