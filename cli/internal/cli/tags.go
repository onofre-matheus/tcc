// `pnn tags` — lista as tags em árvore pela hierarquia `/` (estilo Obsidian),
// com quantos cartões e quantos vencidos cada uma reúne. A contagem de um nó
// usa a mesma regra da sessão personalizada (spec §4.11): `so` reúne os
// cartões de `so` e de `so/…`, sem contar duas vezes o cartão que tem ambas —
// é exatamente o que `pnn revisar --tag so` vai entregar.
package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/spf13/cobra"
)

func newTagsCmd(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "tags",
		Short: "lista as tags em árvore (a/b) com cartões e vencidos de cada uma",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			events, err := a.Store.Events()
			if err != nil {
				return err
			}
			tags, err := core.Tags(events, a.Params())
			if err != nil {
				return err
			}
			if asJSON {
				raw, err := json.MarshalIndent(tags, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(a.Out, string(raw))
				return nil
			}

			th := ui.Theme{On: a.Color}
			if len(tags.Names) == 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim("nenhuma tag — use `pnn carta --tag so` ou `pnn editar N --tag so`"))
				return nil
			}

			decks, err := core.Decks(events)
			if err != nil {
				return err
			}
			study, err := core.Study(events, a.Params()) // sem filtro: universo ativo
			if err != nil {
				return err
			}
			leitner, err := core.Leitner(events, a.Params())
			if err != nil {
				return err
			}
			active := append(slices.Clone(study.Queue), study.Practice...)
			due := map[string]bool{}
			for _, id := range study.Queue {
				due[id] = true
			}

			// Caminhos da árvore: cada tag e seus ancestrais implícitos
			// (`so/escalonamento` sem `so` próprio ainda mostra `so`).
			var paths []string
			for _, name := range tags.Names {
				segments := strings.Split(name, "/")
				for i := range segments {
					if path := strings.Join(segments[:i+1], "/"); !slices.Contains(paths, path) {
						paths = append(paths, path)
					}
				}
			}
			slices.Sort(paths)

			for _, path := range paths {
				match := core.StudyMatcher(decks, core.StudyFilter{Tags: []string{path}})
				cards, dueCards := 0, 0
				for _, id := range active {
					if match(leitner.Cards[id]) {
						cards++
						if due[id] {
							dueCards++
						}
					}
				}
				depth := strings.Count(path, "/")
				label := path[strings.LastIndex(path, "/")+1:]
				if depth == 0 {
					label = "#" + label
				}
				line := fmt.Sprintf(" %s%s  %s", strings.Repeat("  ", depth), label, th.Dim(plural(cards, "cartão", "cartões")))
				if dueCards > 0 {
					line += "  " + th.Red(plural(dueCards, "vencido", "vencidos"))
				}
				fmt.Fprintln(a.Out, line)
			}
			fmt.Fprintf(a.Out, "\n%s\n", th.Dim("estude por tag: pnn revisar --tag <tag> [--treino]"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite a projeção de tags em JSON")
	return cmd
}
