// Sessões salvas (catálogo 1.5, spec §3 e §4.13), como os decks filtrados do
// Anki: `pnn sessao "Nome" [deck] --tag T --ao-redor N [--treino]` guarda o
// filtro da sessão personalizada; `pnn sessoes` lista com o que cada uma
// reúne hoje; `pnn revisar --sessao N|"Nome"` estuda. Guarda o filtro, não a
// lista de cartões — reaberta, a sessão reúne o que casar naquele dia.
package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/onofre-matheus/tcc/cli/store"
	"github.com/spf13/cobra"
)

const kindStudy = "study"

// deckRoots: os decks com exatamente esse nome — a sessão guarda a raiz, e
// a subárvore "::" é expandida na hora de estudar (§4.11).
func deckRoots(decks core.DecksState, name string) []string {
	var ids []string
	for id, deck := range decks.Decks {
		if deck.Name == name {
			ids = append(ids, id)
		}
	}
	return ids
}

func savePayload(id string, s core.SavedStudy) map[string]any {
	return map[string]any{
		"study_id": id, "name": s.Name, "decks": s.Decks, "tags": s.Tags,
		"around": s.Around, "practice": s.Practice,
	}
}

// resolveStudy: o número da última tela (`pnn sessoes`) ou o nome exato.
func resolveStudy(a *app.App, studies core.StudiesState, arg string) (string, core.SavedStudy, error) {
	if n, err := strconv.Atoi(arg); err == nil {
		ref, err := a.ResolveView(n)
		if err != nil {
			return "", core.SavedStudy{}, err
		}
		study, ok := studies.Studies[ref.ID]
		if ref.Kind != kindStudy || !ok {
			return "", core.SavedStudy{}, fmt.Errorf("o item %d da última tela não é uma sessão salva — veja `pnn sessoes`", n)
		}
		return ref.ID, study, nil
	}
	if id, ok := studyByName(studies, arg); ok {
		return id, studies.Studies[id], nil
	}
	return "", core.SavedStudy{}, fmt.Errorf("sessão %q não encontrada — veja `pnn sessoes`", arg)
}

func studyByName(studies core.StudiesState, name string) (string, bool) {
	for _, id := range studies.Order {
		if studies.Studies[id].Name == strings.TrimSpace(name) {
			return id, true
		}
	}
	return "", false
}

// describeStudy resume o filtro em uma linha: "deck SO · #prova-p2 · ao redor de #so · treino".
func describeStudy(g graph, s core.SavedStudy) string {
	var parts []string
	for _, id := range s.Decks {
		if deck, ok := g.decks.Decks[id]; ok {
			parts = append(parts, "deck "+deck.Name)
		} else {
			parts = append(parts, "deck (apagado)")
		}
	}
	for _, tag := range s.Tags {
		parts = append(parts, "#"+tag)
	}
	for _, key := range s.Around {
		if node, ok := core.ParseNode(key); ok {
			parts = append(parts, "ao redor de "+g.label(node))
		}
	}
	if s.Practice {
		parts = append(parts, "treino")
	}
	if len(parts) == 0 {
		return "todos os cartões"
	}
	return strings.Join(parts, " · ")
}

func studyCounts(a *app.App, g graph, s core.SavedStudy) (core.StudyState, error) {
	params := a.Params()
	params.Study = s.Filter()
	return core.Study(g.events, params)
}

func countsLine(st core.StudyState) string {
	return fmt.Sprintf("%s · %d para treinar", plural(len(st.Queue), "vencido", "vencidos"), len(st.Practice))
}

func newSessaoCmd(a *app.App) *cobra.Command {
	var tags, around []string
	var treino bool
	cmd := &cobra.Command{
		Use:   `sessao "Nome" [deck]`,
		Short: "salva o filtro de revisão (deck, --tag, --ao-redor, --treino) com um nome (study.saved)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return fmt.Errorf("informe o nome da sessão")
			}
			g, err := loadGraph(a)
			if err != nil {
				return err
			}
			study := core.SavedStudy{Name: name, Decks: []string{}, Tags: core.NormalizeTags(tags), Around: []string{}, Practice: treino}
			if len(tags) > 0 && len(study.Tags) == 0 {
				return fmt.Errorf("--tag vazia — informe o nome da tag (ex.: --tag so/escalonamento)")
			}
			if len(args) == 2 {
				if study.Decks = deckRoots(g.decks, args[1]); len(study.Decks) == 0 {
					return fmt.Errorf("deck %q não encontrado — veja `pnn decks`", args[1])
				}
			}
			for _, arg := range around {
				node, err := g.resolve(a, arg)
				if err != nil {
					return err
				}
				study.Around = append(study.Around, node.Key())
			}
			if len(study.Decks)+len(study.Tags)+len(study.Around) == 0 && !treino {
				return fmt.Errorf("sessão sem filtro é a revisão de sempre — informe deck, --tag ou --ao-redor (ou --treino)")
			}

			studies, err := core.Studies(g.events)
			if err != nil {
				return err
			}
			verb := "salva"
			id := store.UUIDv7()
			if existing, ok := studyByName(studies, name); ok {
				id, verb = existing, "atualizada" // mesmo nome: substitui o filtro
			}
			if _, err := a.Store.Append("study.saved", savePayload(id, study)); err != nil {
				return err
			}
			st, err := studyCounts(a, g, study)
			if err != nil {
				return err
			}
			th := ui.Theme{On: a.Color}
			fmt.Fprintf(a.Out, "✔ sessão %s: %s — %s\n", verb, name, countsLine(st))
			fmt.Fprintf(a.Out, "  %s\n", th.Dim(describeStudy(g, study)+" · revise: pnn revisar --sessao \""+name+"\""))
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "tag do filtro, incluindo as descendentes (repetível: casa qualquer uma)")
	cmd.Flags().StringSliceVar(&around, "ao-redor", nil, "nó cuja vizinhança entra no filtro (N da última tela, '#tag' ou deck; repetível)")
	cmd.Flags().BoolVar(&treino, "treino", false, "a sessão abre no modo treino (inclui os não vencidos, sem mexer nas caixas)")
	return cmd
}

func newSessoesCmd(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "sessoes",
		Short: "lista as sessões salvas com o que cada uma reúne hoje",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			g, err := loadGraph(a)
			if err != nil {
				return err
			}
			studies, err := core.Studies(g.events)
			if err != nil {
				return err
			}
			if asJSON {
				raw, err := json.MarshalIndent(studies, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(a.Out, string(raw))
				return nil
			}
			th := ui.Theme{On: a.Color}
			if len(studies.Order) == 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim(`nenhuma sessão salva — salve um filtro com `+"`"+`pnn sessao "Véspera da P2" --tag prova-p2`+"`"))
				return a.SaveView(nil)
			}
			refs := make([]app.ViewRef, len(studies.Order))
			for i, id := range studies.Order {
				study := studies.Studies[id]
				refs[i] = app.ViewRef{Kind: kindStudy, ID: id}
				st, err := studyCounts(a, g, study)
				if err != nil {
					return err
				}
				counts := plural(len(st.Queue), "vencido", "vencidos")
				if len(st.Queue) > 0 {
					counts = th.Red(counts)
				}
				fmt.Fprintf(a.Out, " %d %s  %s · %d para treinar\n   %s\n", i+1, th.Bold(study.Name), counts, len(st.Practice), th.Dim(describeStudy(g, study)))
			}
			fmt.Fprintf(a.Out, "\n%s\n", th.Dim(`revise: pnn revisar --sessao N · renomeie: pnn editar N "nome" · apague: pnn apagar N`))
			return a.SaveView(refs)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite a projeção de sessões salvas em JSON")
	return cmd
}

// renomearSessao: study.saved com o mesmo id substitui a sessão inteira, então
// o filtro vai junto, intacto.
func renomearSessao(a *app.App, id, name string) error {
	events, err := a.Store.Events()
	if err != nil {
		return err
	}
	studies, err := core.Studies(events)
	if err != nil {
		return err
	}
	study, ok := studies.Studies[id]
	if !ok {
		return fmt.Errorf("a sessão não existe mais — rode `pnn sessoes`")
	}
	old := study.Name
	if study.Name = strings.TrimSpace(name); study.Name == "" {
		return fmt.Errorf("informe o novo nome da sessão")
	}
	if _, err := a.Store.Append("study.saved", savePayload(id, study)); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "✔ sessão renomeada: %s → %s\n", old, study.Name)
	return nil
}

func apagarSessao(a *app.App, id string) error {
	events, err := a.Store.Events()
	if err != nil {
		return err
	}
	studies, err := core.Studies(events)
	if err != nil {
		return err
	}
	study, ok := studies.Studies[id]
	if !ok {
		return fmt.Errorf("a sessão não existe mais — rode `pnn sessoes`")
	}
	if _, err := a.Store.Append("study.deleted", map[string]any{"study_id": id}); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "✗ sessão apagada: %s (só o filtro — os cartões ficam)\n", study.Name)
	return nil
}
