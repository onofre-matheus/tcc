// Datas civis no fuso do usuário (spec/SPEC.md §4).
// Projeções recebem `now` e `tz`; nada aqui lê o relógio do sistema.
package core

import (
	"fmt"
	"time"
	_ "time/tzdata" // fusos IANA embutidos: o binário não depende do zoneinfo do sistema
)

const civilDate = "2006-01-02"

// LocalDate devolve a data civil (AAAA-MM-DD) do instante UTC no fuso IANA tz.
func LocalDate(tsUTC, tz string) (string, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", fmt.Errorf("fuso inválido %q: %w", tz, err)
	}
	t, err := time.Parse(time.RFC3339, tsUTC)
	if err != nil {
		return "", fmt.Errorf("instante inválido %q: %w", tsUTC, err)
	}
	return t.In(loc).Format(civilDate), nil
}

// AddDays soma dias (podendo ser negativos) a uma data civil AAAA-MM-DD.
func AddDays(isoDate string, days int) (string, error) {
	t, err := time.Parse(civilDate, isoDate)
	if err != nil {
		return "", fmt.Errorf("data inválida %q: %w", isoDate, err)
	}
	return t.AddDate(0, 0, days).Format(civilDate), nil
}
