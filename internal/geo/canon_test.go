package geo

import (
	"os"
	"strings"
	"testing"
)

func TestCanon(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"нижний регистр", "Бородина", "бородина"},
		{"ё заменяется на е", "Улица Ёлок", "улица елок"},
		{"пунктуация в пробелы", "ул. Ленина, д. 5", "ул ленина д 5"},
		{"цифра и буква разделяются", "3я", "3 я"},
		{"дефис перед буквой", "3-я", "3 я"},
		{"пунктуация и цифры", "22А!", "22 а"},
		{"множественные пробелы схлопываются", "  улица   Ленина  ", "улица ленина"},
		{"идемпотентность", Canon("3я линейная"), "3 я линейная"},
		{"белорусские і и ў сохраняются", "вуліца Міцкевіча", "вуліца міцкевіча"},
		{"заглавная Ў", "Ўзгорак", "ўзгорак"},
		{"белорусская цифра и буква", "1і", "1 і"},
		{"украинские ї є ґ сохраняются", "Їжакевича Ґалаґан Євпаторійська", "їжакевича ґалаґан євпаторійська"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Canon(tc.raw); got != tc.want {
				t.Fatalf("Canon(%q) = %q, ожидалось %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestCanonMatchesSQL — нормализация запроса (Go) и индекса (geo.names, SQL) обязаны совпадать:
// расхождение классов символов ломает поиск незаметно для остальных тестов.
func TestCanonMatchesSQL(t *testing.T) {
	script, err := os.ReadFile("../schema/sql/02-names.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{nonNameChars.String(), digitLetter.String()} {
		if got := strings.Count(string(script), "'"+pattern+"'"); got != 2 {
			t.Fatalf("в 02-names.sql шаблон %q встречается %d раз, ожидалось 2 (зоны и улицы)", pattern, got)
		}
	}
}
