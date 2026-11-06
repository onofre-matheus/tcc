// Normalização e entrada de tags (spec/SPEC.md §3) — a mesma tabela roda no
// cliente JS (extension/test/tags.test.js).
package core_test

import (
	"slices"
	"testing"

	"github.com/onofre-matheus/tcc/cli/core"
)

func TestNormalizeTag(t *testing.T) {
	for raw, want := range map[string]string{
		"so":                 "so",
		"#SO":                "so",
		"  ##Prova  P2 ":     "prova-p2",
		"/SO// Threads /":    "so/threads",
		"# Memória Virtual":  "memória-virtual",
		"so / escalonamento": "so/escalonamento",
		"#":                  "",
		"  ":                 "",
		"//":                 "",
	} {
		if got := core.NormalizeTag(raw); got != want {
			t.Errorf("NormalizeTag(%q) = %q, esperado %q", raw, got, want)
		}
	}
}

func TestParseTags(t *testing.T) {
	for input, want := range map[string][]string{
		"#SO #Prova-P2, redes":  {"so", "prova-p2", "redes"},
		"so,so  #so":            {"so"},
		"so/escalonamento,,  ,": {"so/escalonamento"},
		"":                      {},
		"  # , ":                {},
	} {
		if got := core.ParseTags(input); !slices.Equal(got, want) {
			t.Errorf("ParseTags(%q) = %v, esperado %v", input, got, want)
		}
	}
}
