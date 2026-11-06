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

func TestRenameTag(t *testing.T) {
	for _, c := range []struct{ tag, from, to, want string }{
		{"so", "so", "sistemas", "sistemas"},
		{"so/threads/posix", "so", "sistemas", "sistemas/threads/posix"},
		{"sobre", "so", "sistemas", "sobre"}, // prefixo de texto não é descendente
		{"redes", "so", "sistemas", "redes"},
		{"so/threads", "so/threads", "so/concorrencia", "so/concorrencia"},
		{"so/x", "so", "so/base", "so/base/x"}, // renomear para baixo da própria tag
	} {
		if got := core.RenameTag(c.tag, c.from, c.to); got != c.want {
			t.Errorf("RenameTag(%q, %q, %q) = %q, esperado %q", c.tag, c.from, c.to, got, c.want)
		}
	}
}

func TestParseNode(t *testing.T) {
	for key, want := range map[string]string{
		"card:c1":          "card:c1",
		"tag:#SO/Threads ": "tag:so/threads",
		"deck: d1 ":        "deck:d1",
		"tag:a:b":          "tag:a:b", // o id pode ter ":" — corta no primeiro
		"note:n1":          "",        // kind desconhecido
		"tag: # ":          "",        // tag vazia
		"c1":               "",        // sem kind
	} {
		node, ok := core.ParseNode(key)
		got := ""
		if ok {
			got = node.Key()
		}
		if got != want {
			t.Errorf("ParseNode(%q) = %q (ok=%v), esperado %q", key, got, ok, want)
		}
	}
}
