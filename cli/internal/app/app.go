// Contexto de execução da CLI: réplica, relógio, fuso e saída.
// Tudo injetável — os testes montam um App com diretório temporário e relógio
// fixo; produção usa New().
package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/store"
)

type App struct {
	Store *store.Store
	Dir   string // diretório da réplica (~/.pnn)
	TZ    string // fuso IANA do usuário
	Now   func() time.Time
	Out   io.Writer
	Color bool
}

func New() (*App, error) {
	dir := os.Getenv("PNN_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".pnn")
	}
	s, err := store.Open(dir)
	if err != nil {
		return nil, err
	}
	return &App{
		Store: s,
		Dir:   dir,
		TZ:    detectTZ(),
		Now:   time.Now,
		Out:   os.Stdout,
		Color: colorEnabled(),
	}, nil
}

// NowISO devolve o instante corrente em UTC ISO-8601 (formato do envelope).
func (a *App) NowISO() string {
	return a.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// Params são os parâmetros de projeção do observador corrente.
func (a *App) Params() core.Params {
	return core.Params{Now: a.NowISO(), TZ: a.TZ}
}

// Today é a data civil corrente no fuso do usuário.
func (a *App) Today() (string, error) {
	return core.LocalDate(a.NowISO(), a.TZ)
}

func detectTZ() string {
	if tz := os.Getenv("PNN_TZ"); tz != "" {
		return tz
	}
	if tz := os.Getenv("TZ"); strings.Contains(tz, "/") {
		return tz
	}
	if raw, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(raw)); tz != "" {
			return tz
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(target, "/zoneinfo/"); i >= 0 {
			return target[i+len("/zoneinfo/"):]
		}
	}
	return "America/Sao_Paulo"
}

// Cor: nunca é o único sinal (spec/CLI.md §1); respeita NO_COLOR e desliga
// quando a saída não é um terminal.
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
