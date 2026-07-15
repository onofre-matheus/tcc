// Registro de projeções. Cada projeção se registra no próprio arquivo (init),
// então acrescentar uma projeção nova não modifica código existente — o
// harness de vetores e o `--json` da CLI só consultam o registro (OCP).
package core

// Params carrega os parâmetros de projeção que dependem do observador:
// o instante `now` (UTC ISO-8601) e o fuso IANA `tz` (spec/SPEC.md §4).
// Projeções nunca leem o relógio do sistema.
type Params struct {
	Now string
	TZ  string
}

type Projection func(events []Event, p Params) (any, error)

var registry = map[string]Projection{}

func Register(name string, fn Projection) { registry[name] = fn }

func Lookup(name string) (Projection, bool) {
	fn, ok := registry[name]
	return fn, ok
}
