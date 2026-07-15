// Harness dos vetores de conformidade (spec/SPEC.md §6) — mesma bateria que
// roda no cliente JS (extension/test/vectors.test.js): asserção por inclusão
// parcial e reexecução com a entrada embaralhada quando "permutations": true.
package core_test

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/onofre-matheus/tcc/cli/core"
)

const permutationRuns = 5

type vector struct {
	Name         string          `json:"name"`
	Projection   string          `json:"projection"`
	Now          string          `json:"now"`
	TZ           string          `json:"tz"`
	Permutations bool            `json:"permutations"`
	Events       []core.Event    `json:"events"`
	Expected     json.RawMessage `json:"expected"`
}

func TestVectors(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "spec", "vectors", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("nenhum vetor encontrado em spec/vectors")
	}

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var v vector
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("%s: %v", file, err)
		}

		t.Run(filepath.Base(file), func(t *testing.T) {
			fn, ok := core.Lookup(v.Projection)
			if !ok {
				t.Fatalf("projeção não registrada: %q", v.Projection)
			}

			run := func(t *testing.T, events []core.Event) {
				got, err := fn(events, core.Params{Now: v.Now, TZ: v.TZ})
				if err != nil {
					t.Fatal(err)
				}
				assertMatch(t, asJSON(t, got), asJSON(t, v.Expected), "$")
			}

			t.Run("produz o estado esperado", func(t *testing.T) {
				run(t, v.Events)
			})

			if v.Permutations {
				for seed := uint64(1); seed <= permutationRuns; seed++ {
					t.Run(fmt.Sprintf("converge com a entrada embaralhada (permutação %d)", seed), func(t *testing.T) {
						events := append([]core.Event(nil), v.Events...)
						rng := rand.New(rand.NewPCG(seed, 0))
						rng.Shuffle(len(events), func(i, j int) {
							events[i], events[j] = events[j], events[i]
						})
						run(t, events)
					})
				}
			}
		})
	}
}

// asJSON normaliza qualquer valor para a forma genérica de JSON decodificado
// (map[string]any, []any, float64, string, bool, nil), para que a comparação
// não dependa dos tipos concretos das projeções.
func asJSON(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// assertMatch aplica a inclusão parcial da spec: todo campo presente em
// expected deve ter valor idêntico; campos ausentes são livres; arrays são
// comparados por igualdade exata.
func assertMatch(t *testing.T, actual, expected any, path string) {
	t.Helper()
	switch exp := expected.(type) {
	case []any:
		if !reflect.DeepEqual(actual, exp) {
			t.Fatalf("%s: esperado %v, veio %v", path, exp, actual)
		}
	case map[string]any:
		act, ok := actual.(map[string]any)
		if !ok {
			t.Fatalf("%s: esperado objeto, veio %T (%v)", path, actual, actual)
		}
		for key, val := range exp {
			assertMatch(t, act[key], val, path+"."+key)
		}
	default:
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("%s: esperado %v (%T), veio %v (%T)", path, expected, expected, actual, actual)
		}
	}
}
