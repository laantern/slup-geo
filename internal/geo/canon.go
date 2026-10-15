package geo

import (
	"regexp"
	"strings"
)

// Каноническая нормализация имени — та же, что в geo.names (02-names.sql):
// lower, ё→е, пунктуация → пробелы, пробел между цифрой и буквой («3я» = «3-я» = «3 я»).
// Буквы вне диапазона а–я (белорусские і/ў, украинские ї/є/ґ) — часть имени, а не пунктуация:
// иначе «вуліца Міцкевіча» превращалась бы в «вул ца м цкев ча».
// Канон идемпотентен: canon(canon(x)) == canon(x).
var (
	nonNameChars = regexp.MustCompile(`[^а-яіўїєґa-z0-9]+`)
	digitLetter  = regexp.MustCompile(`([0-9])([а-яіўїєґa-z])`)
	whitespace   = regexp.MustCompile(`\s+`)
)

// Canon нормализует строку запроса (и имена в индексе) к поисковой форме.
func Canon(raw string) string {
	lowered := strings.ReplaceAll(strings.ToLower(raw), "ё", "е")
	cleaned := nonNameChars.ReplaceAllString(lowered, " ")
	split := digitLetter.ReplaceAllString(cleaned, "$1 $2")
	return whitespace.ReplaceAllString(strings.TrimSpace(split), " ")
}
