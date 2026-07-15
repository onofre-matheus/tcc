// Números efêmeros (spec/CLI.md §1, estilo Taskwarrior): a tela do dia numera
// itens 1..n e o mapeamento número→id fica em last-view.json, para que
// `pnn feito 2` ou `pnn foco 1` resolvam sem o usuário digitar UUIDs.
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const viewFile = "last-view.json"

type ViewRef struct {
	Kind string `json:"kind"` // "task" (cartões e notas ganham kinds próprios depois)
	ID   string `json:"id"`
}

// SaveView grava o mapeamento da tela recém-renderizada; refs[0] é o item 1.
func (a *App) SaveView(refs []ViewRef) error {
	raw, err := json.Marshal(refs)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(a.Dir, viewFile), raw, 0o644)
}

// ResolveView traduz um número efêmero para o item da última tela.
func (a *App) ResolveView(n int) (ViewRef, error) {
	raw, err := os.ReadFile(filepath.Join(a.Dir, viewFile))
	if errors.Is(err, fs.ErrNotExist) {
		return ViewRef{}, errors.New("nenhuma tela anterior — rode `pnn dia` primeiro")
	}
	if err != nil {
		return ViewRef{}, err
	}
	var refs []ViewRef
	if err := json.Unmarshal(raw, &refs); err != nil {
		return ViewRef{}, err
	}
	if n < 1 || n > len(refs) {
		return ViewRef{}, fmt.Errorf("número %d fora da última tela (1..%d) — rode `pnn dia`", n, len(refs))
	}
	return refs[n-1], nil
}
