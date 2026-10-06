package geo

import "testing"

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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Canon(tc.raw); got != tc.want {
				t.Fatalf("Canon(%q) = %q, ожидалось %q", tc.raw, got, tc.want)
			}
		})
	}
}
