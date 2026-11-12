// O acervo por trás do modo de escrita do `pnn foco` (tui/escrita.go): ler e
// gravar a página, e os comandos "/" — criar e concluir tarefa, classificar,
// ligar. Tudo vira evento que já existe (task.created, task.completed,
// page.edited, link.created); o TUI só desenha e chama.
package cli

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/tui"
	"github.com/onofre-matheus/tcc/cli/store"
)

type writeActions struct{ a *app.App }

var _ tui.WriteActions = writeActions{}

func (w writeActions) Load(id string) (tui.PageText, error) {
	page, err := pageByRef(w.a, id)
	if err != nil {
		return tui.PageText{}, err
	}
	return tui.PageText{Title: page.Title, Body: page.Body, Version: page.Version}, nil
}

// Save grava só se o texto mudou em relação à versão atual; a base é a que o
// editor leu (spec §3, Páginas: a detecção de conflito lê base).
func (w writeActions) Save(id, body, base string) (string, error) {
	page, err := pageByRef(w.a, id)
	if err != nil {
		return "", err
	}
	body = cleanBody(body)
	if body == cleanBody(page.Body) {
		return page.Version, nil
	}
	event, err := w.a.Store.Append("page.edited", map[string]any{"page_id": id, "body": body, "base": base})
	if err != nil {
		return "", err
	}
	return event.ID, nil
}

func (w writeActions) Refs(id string) ([]string, error) {
	g, err := loadGraph(w.a)
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, nb := range g.neighborhood(pageNode(id)) {
		refs = append(refs, g.label(nb.node))
	}
	return refs, nil
}

// CreatePage cria a página e a liga à tarefa em foco (se houver): é o
// /pagina — criar e já começar a escrever, sem sair do foco.
func (w writeActions) CreatePage(taskID, title string) (tui.LinkedPage, string, error) {
	title = strings.TrimSpace(title)
	pageID := store.UUIDv7()
	if _, err := w.a.Store.Append("page.created", map[string]any{"page_id": pageID, "title": title, "body": "", "tags": []string{}}); err != nil {
		return tui.LinkedPage{}, "", err
	}
	page := tui.LinkedPage{ID: pageID, Title: title}
	if taskID == "" {
		return page, "✔ página criada: " + title, nil
	}
	if _, err := w.a.Store.Append("link.created", map[string]any{
		"link_id": store.UUIDv7(), "from": nodePayload(core.Node{Kind: core.NodeTask, ID: taskID}), "to": nodePayload(pageNode(pageID)),
	}); err != nil {
		return tui.LinkedPage{}, "", err
	}
	return page, "✔ página criada e ligada à tarefa: " + title, nil
}

// CreateTask cria a tarefa: subtarefa da tarefa em foco (parentID) — a
// decomposição que a spec recomenda para "não sei por onde começar" — e/ou
// ligada à página aberta (pageID).
func (w writeActions) CreateTask(parentID, pageID, title string) (string, error) {
	title = strings.TrimSpace(title)
	taskID := store.UUIDv7()
	payload := map[string]any{"task_id": taskID, "title": title}
	if parentID != "" {
		payload["parent_id"] = parentID
	}
	if _, err := w.a.Store.Append("task.created", payload); err != nil {
		return "", err
	}
	if pageID != "" {
		if _, err := w.a.Store.Append("link.created", map[string]any{
			"link_id": store.UUIDv7(), "from": nodePayload(pageNode(pageID)), "to": nodePayload(core.Node{Kind: core.NodeTask, ID: taskID}),
		}); err != nil {
			return "", err
		}
	}
	switch {
	case parentID != "" && pageID != "":
		return "✔ subtarefa criada e ligada à página: " + title, nil
	case parentID != "":
		return "✔ subtarefa criada: " + title, nil
	case pageID != "":
		return "✔ tarefa criada e ligada à página: " + title, nil
	}
	return "✔ tarefa criada: " + title, nil
}

// Tasks: as abertas — a em foco primeiro, depois as ligadas à página (⚡),
// depois as do dia.
func (w writeActions) Tasks(pageID, focusID string) ([]tui.Choice, error) {
	g, err := loadGraph(w.a)
	if err != nil {
		return nil, err
	}
	var out []tui.Choice
	seen := map[string]bool{}
	add := func(id, mark string) {
		task, ok := g.tasks.Tasks[id]
		if !ok || task.Done || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, tui.Choice{Kind: core.NodeTask, ID: id, Label: "[" + priorityLabel(task) + "] " + task.Title + mark})
	}
	if focusID != "" {
		add(focusID, " (em foco)")
	}
	if pageID != "" {
		for _, nb := range g.neighborhood(pageNode(pageID)) {
			if nb.node.Kind == core.NodeTask {
				add(nb.node.ID, " ⚡")
			}
		}
	}
	for _, id := range g.tasks.DayList {
		add(id, "")
	}
	return out, nil
}

func (w writeActions) CompleteTask(taskID string) (string, error) {
	g, err := loadGraph(w.a)
	if err != nil {
		return "", err
	}
	task, ok := g.tasks.Tasks[taskID]
	if !ok {
		return "", fmt.Errorf("a tarefa não existe mais")
	}
	if task.Done {
		return "· já estava concluída: " + task.Title, nil
	}
	if _, err := w.a.Store.Append("task.completed", map[string]any{"task_id": taskID}); err != nil {
		return "", err
	}
	return "✔ concluída: " + task.Title, nil
}

func (w writeActions) Tag(pageID string, add, remove []string) (string, error) {
	page, err := pageByRef(w.a, pageID)
	if err != nil {
		return "", err
	}
	next := tagEdit{add: add, untag: remove}.apply(page.Tags)
	if slices.Equal(next, page.Tags) {
		return "· nada mudou nas tags", nil
	}
	if _, err := w.a.Store.Append("page.edited", map[string]any{"page_id": pageID, "tags": next}); err != nil {
		return "", err
	}
	if len(next) == 0 {
		return "🏷 página sem tags", nil
	}
	return "🏷 tags da página: " + hashTags(next), nil
}

// Search: páginas e cartões com todas as palavras, como o `pnn buscar` — sem
// o próprio nó; até 9, que é o que o seletor numera.
func (w writeActions) Search(excludeID, query string) ([]tui.Choice, error) {
	g, err := loadGraph(w.a)
	if err != nil {
		return nil, err
	}
	pageIDs, cardIDs := searchAcervo(g, strings.Fields(fold(query)))
	var out []tui.Choice
	for _, id := range pageIDs {
		if id != excludeID {
			out = append(out, tui.Choice{Kind: core.NodePage, ID: id, Label: g.label(pageNode(id))})
		}
	}
	for _, id := range cardIDs {
		out = append(out, tui.Choice{Kind: core.NodeCard, ID: id, Label: g.label(core.Node{Kind: core.NodeCard, ID: id})})
	}
	return out[:min(len(out), 9)], nil
}

// Link cria a sinapse do nó de referência (a página aberta ou a tarefa em
// foco) para o escolhido.
func (w writeActions) Link(from, target tui.Choice) (string, error) {
	g, err := loadGraph(w.a)
	if err != nil {
		return "", err
	}
	src, to := core.Node{Kind: from.Kind, ID: from.ID}, core.Node{Kind: target.Kind, ID: target.ID}
	if g.linkedAs(src, to, "") {
		return "· já há sinapse com " + g.label(to), nil
	}
	if _, err := w.a.Store.Append("link.created", map[string]any{
		"link_id": store.UUIDv7(), "from": nodePayload(src), "to": nodePayload(to),
	}); err != nil {
		return "", err
	}
	return "⚡ sinapse: " + g.label(src) + " → " + g.label(to), nil
}

// CreateCard cria um cartão solto (card.created v2, sem deck — como o `pnn
// carta` em "(sem deck)") e o liga ao nó de referência: estudar ao redor da
// página ou da tarefa o traz.
func (w writeActions) CreateCard(from tui.Choice, front, back string) (string, error) {
	cardID := store.UUIDv7()
	if _, err := w.a.Store.AppendV("card.created", 2, map[string]any{
		"card_id": cardID, "front": front, "back": back, "tags": []string{},
	}); err != nil {
		return "", err
	}
	if _, err := w.a.Store.Append("link.created", map[string]any{
		"link_id": store.UUIDv7(), "from": nodePayload(core.Node{Kind: from.Kind, ID: from.ID}), "to": nodePayload(core.Node{Kind: core.NodeCard, ID: cardID}),
	}); err != nil {
		return "", err
	}
	where := "à página"
	if from.Kind == core.NodeTask {
		where = "à tarefa"
	}
	return "✔ cartão criado e ligado " + where + ": " + front, nil
}

// CaptureNote é o /distracao fora do foco (pnn escrever): sem sessão para a
// distração pertencer, o pensamento solto vai para a caixa de entrada, como
// o `pnn n`.
func (w writeActions) CaptureNote(text string) (string, error) {
	if _, err := w.a.Store.Append("note.captured", map[string]any{"note_id": store.UUIDv7(), "text": text}); err != nil {
		return "", err
	}
	return "✔ nota na caixa de entrada — triagem depois", nil
}

func newEscreverCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   `escrever N|título`,
		Short: "abre a página no editor do pnn, com os comandos / (sem sessão de foco)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			g, err := loadGraph(a)
			if err != nil {
				return err
			}
			id, err := findPage(a, g, strings.Join(args, " "))
			if err != nil || id == "" {
				return err
			}
			model := tui.NewFoco(tui.FocoConfig{
				Standalone:   true,
				StartWriting: true,
				Pages:        []tui.LinkedPage{{ID: id, Title: g.pages.Pages[id].Title}},
				Write:        writeActions{a},
				EditPage:     focusEditPage(a),
				Emit:         emitTo(a),
			})
			final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
			if err != nil {
				return err
			}
			foco := final.(tui.Foco)
			if foco.Err != nil {
				return foco.Err
			}
			if status := foco.Status(); status != "" {
				fmt.Fprintln(a.Out, status)
			}
			return nil
		},
	}
}
