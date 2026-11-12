// Páginas (catálogo 1.6, spec §3 "Páginas" e §4.15): anotações longas e de
// uso constante. O $EDITOR do usuário abre só o corpo; título, tags e
// sinapses são metadado, mudam por comando (`pnn editar N "título"`,
// `--tag`, `pnn ligar`) e a página os mostra como referência — numerados,
// para navegar. `pnn p "Título"` cria; `pnn paginas` lista (fixadas
// primeiro); `pnn pagina N|título` lê (-e edita); `pnn buscar` procura em
// páginas e cartões; `pnn fixar N` fixa no topo.
package cli

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/onofre-matheus/tcc/cli/store"
	"github.com/spf13/cobra"
)

func loadPages(a *app.App) (core.PagesState, error) {
	events, err := a.Store.Events()
	if err != nil {
		return core.PagesState{}, err
	}
	return core.Pages(events)
}

func pageByRef(a *app.App, id string) (core.Page, error) {
	state, err := loadPages(a)
	if err != nil {
		return core.Page{}, err
	}
	page, ok := state.Pages[id]
	if !ok {
		return core.Page{}, fmt.Errorf("a página não existe mais — rode `pnn paginas`")
	}
	return page, nil
}

// relDay diz quando, em relação a hoje no fuso do usuário: hoje, ontem,
// há N dias ou a data.
func relDay(a *app.App, ts string) string {
	day, err := core.LocalDate(ts, a.TZ)
	if err != nil {
		return ""
	}
	today, err := a.Today()
	if err != nil {
		return day
	}
	d, _ := time.Parse("2006-01-02", day)
	t, _ := time.Parse("2006-01-02", today)
	switch n := int(t.Sub(d).Hours() / 24); {
	case n <= 0:
		return "hoje"
	case n == 1:
		return "ontem"
	case n < 7:
		return fmt.Sprintf("há %d dias", n)
	}
	if d.Year() == t.Year() {
		return d.Format("02/01")
	}
	return d.Format("02/01/2006")
}

func pageNode(id string) core.Node { return core.Node{Kind: core.NodePage, ID: id} }

// pageMeta: "#tags · editada hoje · ⚡2" (ou "arquivada"). conns = quantas
// sinapses a página tem; 0 omite.
func pageMeta(a *app.App, page core.Page, conns int) string {
	var parts []string
	if len(page.Tags) > 0 {
		parts = append(parts, hashTags(page.Tags))
	}
	if page.Archived {
		parts = append(parts, "arquivada")
	} else {
		parts = append(parts, "editada "+relDay(a, page.UpdatedAt))
	}
	if conns > 0 {
		parts = append(parts, fmt.Sprintf("⚡%d", conns))
	}
	return strings.Join(parts, " · ")
}

func (g graph) pageConns(id string) int { return len(g.links.Neighbors[pageNode(id).Key()]) }

func pageTitle(page core.Page) string {
	if page.Pinned {
		return "📌 " + page.Title
	}
	return page.Title
}

// printPageRows lista as páginas numeradas e grava a última tela.
func printPageRows(a *app.App, g graph, ids []string) error {
	th := ui.Theme{On: a.Color}
	refs := make([]app.ViewRef, len(ids))
	for i, id := range ids {
		page := g.pages.Pages[id]
		refs[i] = app.ViewRef{Kind: core.NodePage, ID: id}
		fmt.Fprintf(a.Out, " %d  %s  %s\n", i+1, pageTitle(page), th.Dim(pageMeta(a, page, g.pageConns(id))))
	}
	return a.SaveView(refs)
}

func newPCmd(a *app.App) *cobra.Command {
	var tags []string
	cmd := &cobra.Command{
		Use:   `p "título"`,
		Short: "cria uma página e abre o corpo no seu $EDITOR (page.created)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			title := strings.TrimSpace(args[0])
			if title == "" {
				return fmt.Errorf(`informe o título: pnn p "Título"`)
			}
			pageTags := core.NormalizeTags(tags)
			raw, path, err := editInEditor(a, title, "")
			if err != nil {
				return err
			}
			body := cleanBody(raw)

			id := store.UUIDv7()
			if _, err := a.Store.Append("page.created", map[string]any{
				"page_id": id, "title": title, "body": body, "tags": pageTags,
			}); err != nil {
				return keptAt(err, path)
			}
			os.Remove(path)

			th := ui.Theme{On: a.Color}
			fmt.Fprintf(a.Out, "✔ página criada: %s", title)
			if len(pageTags) > 0 {
				fmt.Fprintf(a.Out, "  %s", hashTags(pageTags))
			}
			fmt.Fprintf(a.Out, "\n%s\n", th.Dim("→ pnn pagina 1 para ler · pnn ligar 1 N para ligar · pnn fixar 1 para deixar no topo"))
			return a.SaveView([]app.ViewRef{{Kind: core.NodePage, ID: id}})
		},
	}
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "tag da página (repetível; depois: pnn editar N --tag/--untag)")
	return cmd
}

// A lista de páginas cresce sem parar (é o acervo de uso constante): por
// padrão ela mostra as fixadas e as editadas por último, e diz quantas
// ficaram de fora e como chegar nelas. --todas tira o limite.
const (
	pageListLimit = 15 // não fixadas na lista simples
	pageTagLimit  = 5  // por tag, na lista agrupada
)

func newPaginasCmd(a *app.App) *cobra.Command {
	var asJSON, arquivadas, todas, porTag bool
	var tags []string
	cmd := &cobra.Command{
		Use:   "paginas",
		Short: "lista as páginas: as fixadas e as editadas por último (--todas, --por-tag, --tag)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			g, err := loadGraph(a)
			if err != nil {
				return err
			}
			state := g.pages
			if asJSON {
				raw, err := json.MarshalIndent(state, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(a.Out, string(raw))
				return nil
			}

			ids := slices.Clone(state.Order)
			if arquivadas { // só as arquivadas, as mais recentes primeiro
				ids = nil
				for id, page := range state.Pages {
					if page.Archived {
						ids = append(ids, id)
					}
				}
				slices.SortFunc(ids, func(x, y string) int {
					return cmp.Or(strings.Compare(state.Pages[y].UpdatedAt, state.Pages[x].UpdatedAt), strings.Compare(x, y))
				})
			}
			filter := core.NormalizeTags(tags)
			if len(filter) > 0 {
				ids = slices.DeleteFunc(ids, func(id string) bool {
					return !slices.ContainsFunc(state.Pages[id].Tags, func(tag string) bool { return tagUnderAny(tag, filter) })
				})
			}

			th := ui.Theme{On: a.Color}
			if len(ids) == 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim("nenhuma página — crie com `pnn p \"Título\"`"))
				return a.SaveView(nil)
			}
			if porTag {
				return printPagesByTag(a, g, ids, filter, todas)
			}

			// As fixadas sempre aparecem; das demais, as mais recentes.
			shown, hidden := ids, 0
			if !todas {
				pinned := 0 // as fixadas abrem a lista (state.Order); arquivada não conta
				for !arquivadas && pinned < len(ids) && state.Pages[ids[pinned]].Pinned {
					pinned++
				}
				if rest := len(ids) - pinned; rest > pageListLimit {
					hidden = rest - pageListLimit
					shown = ids[:pinned+pageListLimit]
				}
			}
			if err := printPageRows(a, g, shown); err != nil {
				return err
			}
			if hidden > 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim(fmt.Sprintf("… e mais %d — `pnn paginas --todas`, `--por-tag`, `--tag T` ou `pnn buscar`", hidden)))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite a projeção de páginas em JSON")
	cmd.Flags().BoolVar(&todas, "todas", false, "lista todas, sem o limite das mais recentes")
	cmd.Flags().BoolVar(&porTag, "por-tag", false, "agrupa as páginas na árvore de tags (como pastas)")
	cmd.Flags().BoolVar(&arquivadas, "arquivadas", false, "lista só as páginas arquivadas")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "filtra por tag, incluindo as descendentes (repetível: casa qualquer uma)")
	return cmd
}

func tagUnderAny(tag string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(p string) bool { return core.TagUnder(tag, p) })
}

// printPagesByTag mostra as páginas na árvore de tags, como pastas: cada tag
// (e os ancestrais implícitos) com quantas páginas tem na subárvore e, logo
// abaixo, as suas — até pageTagLimit, com o caminho para o resto. A página
// com duas tags aparece nas duas com o mesmo número; as sem tag vão no fim.
// Com --tag, só a subárvore filtrada.
func printPagesByTag(a *app.App, g graph, ids, filter []string, all bool) error {
	th := ui.Theme{On: a.Color}
	byTag := map[string][]string{} // tag exata → páginas, na ordem da lista
	var untagged, paths []string
	for _, id := range ids {
		pageTags := g.pages.Pages[id].Tags
		if len(pageTags) == 0 {
			untagged = append(untagged, id)
		}
		for _, tag := range pageTags {
			byTag[tag] = append(byTag[tag], id)
			segments := strings.Split(tag, "/")
			for i := range segments {
				if path := strings.Join(segments[:i+1], "/"); !slices.Contains(paths, path) {
					paths = append(paths, path)
				}
			}
		}
	}
	if len(filter) > 0 {
		paths = slices.DeleteFunc(paths, func(path string) bool { return !tagUnderAny(path, filter) })
	}
	slices.Sort(paths)

	var refs []app.ViewRef
	num := map[string]int{}
	numberOf := func(id string) int {
		if n, ok := num[id]; ok {
			return n
		}
		refs = append(refs, app.ViewRef{Kind: core.NodePage, ID: id})
		num[id] = len(refs)
		return len(refs)
	}
	printGroup := func(indent string, pages []string, more string) {
		shown, hidden := pages, 0
		if !all && len(pages) > pageTagLimit {
			shown, hidden = pages[:pageTagLimit], len(pages)-pageTagLimit
		}
		for _, id := range shown {
			page := g.pages.Pages[id]
			bare := page
			bare.Tags = nil // a tag já é o título do grupo
			fmt.Fprintf(a.Out, "%s%d  %s  %s\n", indent, numberOf(id), pageTitle(page), th.Dim(pageMeta(a, bare, g.pageConns(id))))
		}
		if hidden > 0 {
			fmt.Fprintf(a.Out, "%s%s\n", indent, th.Dim(fmt.Sprintf("… e mais %d — %s", hidden, more)))
		}
	}

	for _, path := range paths {
		depth := strings.Count(path, "/")
		label := path[strings.LastIndex(path, "/")+1:]
		if depth == 0 {
			label = "#" + label
		}
		count := 0
		for _, id := range ids {
			if slices.ContainsFunc(g.pages.Pages[id].Tags, func(t string) bool { return core.TagUnder(t, path) }) {
				count++
			}
		}
		fmt.Fprintf(a.Out, " %s%s  %s\n", strings.Repeat("  ", depth), th.Bold(label), th.Dim(plural(count, "página", "páginas")))
		printGroup(" "+strings.Repeat("  ", depth+1), byTag[path], fmt.Sprintf("`pnn paginas --tag %s`", path))
	}
	if len(untagged) > 0 && len(filter) == 0 {
		fmt.Fprintf(a.Out, " %s  %s\n", th.Bold("(sem tag)"), th.Dim(plural(len(untagged), "página", "páginas")))
		printGroup("   ", untagged, "`pnn paginas --todas`")
	}
	return a.SaveView(refs)
}

// findPage resolve "N" (última tela) ou parte do título, sem caixa nem
// acento e com todas as palavras. Título idêntico vence; várias candidatas
// viram uma lista numerada e id "" (quem chama para sem erro).
func findPage(a *app.App, g graph, query string) (string, error) {
	if n, err := strconv.Atoi(query); err == nil {
		ref, err := a.ResolveView(n)
		if err != nil {
			return "", err
		}
		if _, ok := g.pages.Pages[ref.ID]; ref.Kind != core.NodePage || !ok {
			return "", fmt.Errorf("o item %d da última tela não é uma página — veja `pnn paginas`", n)
		}
		return ref.ID, nil
	}

	words := strings.Fields(fold(query))
	if len(words) == 0 {
		return "", fmt.Errorf("informe o número ou parte do título da página")
	}
	var found []string
	for _, id := range g.pages.Order {
		title := fold(g.pages.Pages[id].Title)
		if title == strings.Join(words, " ") {
			return id, nil
		}
		if containsAll(title, words) {
			found = append(found, id)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("nenhuma página com %q no título — procure no texto com `pnn buscar %s`", query, query)
	case 1:
		return found[0], nil
	}
	th := ui.Theme{On: a.Color}
	fmt.Fprintf(a.Out, "%s\n", th.Dim(fmt.Sprintf("%d páginas casam com %q — escolha com `pnn pagina N`:", len(found), query)))
	return "", printPageRows(a, g, found)
}

func containsAll(hay string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

func newPaginaCmd(a *app.App) *cobra.Command {
	var editar, cru bool
	cmd := &cobra.Command{
		Use:   `pagina N|título`,
		Short: "mostra a página (N da última tela ou pelo título) com o que está ligado a ela; -e abre no editor",
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

			// A página é o 1; o que está ligado a ela, de 2 em diante — para
			// `pnn pagina N`, `pnn editar N`, `pnn desligar 1 N` sem redigitar.
			refs := []app.ViewRef{{Kind: core.NodePage, ID: id}}
			var conns []string
			for _, nb := range g.neighborhood(pageNode(id)) {
				refs = append(refs, app.ViewRef{Kind: nb.node.Kind, ID: nb.node.ID})
				conn := fmt.Sprintf("%d %s %s", len(refs), nb.arrow, g.label(nb.node))
				if len(nb.relations) > 0 {
					conn += " (" + strings.Join(nb.relations, ", ") + ")"
				}
				conns = append(conns, conn)
			}
			if err := a.SaveView(refs); err != nil {
				return err
			}
			if editar {
				return editPage(a, id)
			}
			page := g.pages.Pages[id]
			if cru {
				_, err := fmt.Fprintln(a.Out, page.Body)
				return err
			}

			th := ui.Theme{On: a.Color}
			var b strings.Builder
			fmt.Fprintf(&b, "%s\n", th.Title(pageTitle(page)))
			fmt.Fprintf(&b, "%s\n", th.Dim(pageMeta(a, page, 0)+" · criada "+relDay(a, page.CreatedAt)))
			if len(conns) > 0 {
				fmt.Fprintf(&b, "⚡ %s\n", strings.Join(conns, "  "))
			}
			if page.Body == "" {
				fmt.Fprintf(&b, "\n%s\n", th.Dim("(página vazia — escreva com `pnn editar 1`)"))
			} else {
				b.WriteString(renderMarkdown(a, page.Body))
			}
			return pageOut(a, b.String())
		},
	}
	cmd.Flags().BoolVarP(&editar, "editar", "e", false, "abre a página no editor")
	cmd.Flags().BoolVar(&cru, "cru", false, "imprime o markdown sem formatar (para redirecionar a um arquivo)")
	return cmd
}

// pageEdit é o resultado de editar o corpo: a linha principal e os avisos.
// Quem chama decide onde mostrar — no terminal (pnn editar) ou dentro da
// tela de foco, que não pode imprimir por cima dela.
type pageEdit struct {
	status   string // "✔ página salva: X" ou "nada mudou: X"
	warnings []string
	empty    bool // a página ficou vazia
}

// editPageBody abre o corpo da página no editor e, se mudou, grava um
// page.edited com `base` = a versão lida (spec §3, Páginas).
func editPageBody(a *app.App, id string) (pageEdit, error) {
	page, err := pageByRef(a, id)
	if err != nil {
		return pageEdit{}, err
	}
	raw, path, err := editInEditor(a, page.Title, bodyFile(page.Body))
	if err != nil {
		return pageEdit{}, err
	}
	body := cleanBody(raw)
	if body == cleanBody(page.Body) {
		os.Remove(path)
		return pageEdit{status: "nada mudou: " + page.Title}, nil
	}

	// Aviso barato de edição concorrente (sync ou outro terminal no meio);
	// a detecção de conflito de verdade lê `base` e fica para depois.
	var edit pageEdit
	if now, err := pageByRef(a, id); err == nil && now.Version != page.Version {
		edit.warnings = append(edit.warnings, "a página mudou enquanto você editava — a sua versão é a que vale agora; a outra continua no log")
	}
	if _, err := a.Store.Append("page.edited", map[string]any{
		"page_id": id, "body": body, "base": page.Version,
	}); err != nil {
		return pageEdit{}, keptAt(err, path)
	}
	os.Remove(path)
	edit.status = "✔ página salva: " + page.Title
	edit.empty = body == ""
	return edit, nil
}

// editPage é o `pnn editar N` / `pnn pagina -e` de uma página: edita e
// imprime o resultado.
func editPage(a *app.App, id string) error {
	edit, err := editPageBody(a, id)
	if err != nil {
		return err
	}
	th := ui.Theme{On: a.Color}
	for _, w := range edit.warnings {
		fmt.Fprintf(a.Out, "%s\n", th.Warn("⚠ "+w))
	}
	fmt.Fprintln(a.Out, edit.status)
	if edit.empty {
		fmt.Fprintf(a.Out, "%s\n", th.Dim("a página ficou vazia — a versão anterior continua no log"))
	}
	return nil
}

// editarPaginaCampos: `pnn editar N "novo título"` e/ou --tag/--untag/--tags
// — o metadado da página muda por comando, nunca pelo texto.
func editarPaginaCampos(a *app.App, id, title string, tags tagEdit) error {
	page, err := pageByRef(a, id)
	if err != nil {
		return err
	}
	payload := map[string]any{"page_id": id}
	if title = strings.TrimSpace(title); title != "" && title != page.Title {
		payload["title"] = title
		page.Title = title
	}
	if tags.any() {
		if next := tags.apply(page.Tags); !slices.Equal(next, page.Tags) {
			payload["tags"] = next
			page.Tags = next
		}
	}
	if len(payload) == 1 {
		fmt.Fprintf(a.Out, "nada mudou: %s\n", page.Title)
		return nil
	}
	if _, err := a.Store.Append("page.edited", payload); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "✔ página editada: %s", page.Title)
	if _, ok := payload["tags"]; ok {
		if len(page.Tags) == 0 {
			fmt.Fprint(a.Out, "  (sem tags)")
		} else {
			fmt.Fprintf(a.Out, "  %s", hashTags(page.Tags))
		}
	}
	fmt.Fprintln(a.Out)
	return nil
}

func newFixarCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "fixar N",
		Short: "fixa a página N no topo de `pnn paginas` (ou desafixa, se já estiver fixada)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ref, err := resolveRef(a, args[0])
			if err != nil {
				return err
			}
			if ref.Kind != core.NodePage {
				return fmt.Errorf("só páginas se fixam — veja `pnn paginas`")
			}
			page, err := pageByRef(a, ref.ID)
			if err != nil {
				return err
			}
			pinned := !page.Pinned
			if _, err := a.Store.Append("page.edited", map[string]any{"page_id": ref.ID, "pinned": pinned}); err != nil {
				return err
			}
			if pinned {
				fmt.Fprintf(a.Out, "📌 fixada: %s\n", page.Title)
			} else {
				fmt.Fprintf(a.Out, "desafixada: %s\n", page.Title)
			}
			return nil
		},
	}
}

// --- busca ---

// snippet devolve o trecho da primeira linha que contém a palavra, com a
// palavra destacada e reticências quando o trecho é cortado.
func snippet(th ui.Theme, text string, word []rune) string {
	for _, line := range strings.Split(text, "\n") {
		rs, fr := []rune(line), foldRunes(line)
		i := indexRunes(fr, word)
		if i < 0 {
			continue
		}
		start, end := max(0, i-30), min(len(rs), i+len(word)+40)
		pre, post := "", ""
		if start > 0 {
			pre = "…"
		}
		if end < len(rs) {
			post = "…"
		}
		return strings.TrimSpace(pre + string(rs[start:i]) + th.Bold(string(rs[i:i+len(word)])) + string(rs[i+len(word):end]) + post)
	}
	return ""
}

func indexRunes(hay, needle []rune) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if slices.Equal(hay[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

// searchAcervo: as páginas (as que casam no título primeiro, na ordem de
// `pnn paginas`) e os cartões ativos (pela frente) que têm todas as palavras
// — já dobradas — no título, nas tags ou no texto. É a busca do `pnn buscar` e
// do `/ligar` do modo de escrita.
func searchAcervo(g graph, words []string) (pageIDs, cardIDs []string) {
	var titleHits, bodyHits []string
	for _, id := range g.pages.Order {
		page := g.pages.Pages[id]
		switch {
		case containsAll(fold(page.Title), words):
			titleHits = append(titleHits, id)
		case containsAll(fold(page.Title+"\n"+hashTags(page.Tags)+"\n"+page.Body), words):
			bodyHits = append(bodyHits, id)
		}
	}
	for id, card := range g.leitner.Cards {
		if card.Archived || g.decks.Decks[card.Deck()].Archived {
			continue
		}
		tags := core.EffectiveTags(card, g.decks)
		if containsAll(fold(card.Front+"\n"+card.Back+"\n"+hashTags(tags)), words) {
			cardIDs = append(cardIDs, id)
		}
	}
	slices.SortFunc(cardIDs, func(x, y string) int {
		return strings.Compare(g.leitner.Cards[x].Front, g.leitner.Cards[y].Front)
	})
	return append(titleHits, bodyHits...), cardIDs
}

func newBuscarCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   `buscar "texto"`,
		Short: "procura nas páginas (título, tags e texto) e nos cartões, sem caixa nem acento",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			words := strings.Fields(fold(query))
			if len(words) == 0 {
				return fmt.Errorf("informe o que procurar")
			}
			first := []rune(words[0])

			g, err := loadGraph(a)
			if err != nil {
				return err
			}

			pageIDs, cardIDs := searchAcervo(g, words)

			th := ui.Theme{On: a.Color}
			if len(pageIDs)+len(cardIDs) == 0 {
				fmt.Fprintf(a.Out, "%s\n", th.Dim(fmt.Sprintf("nada encontrado para %q", query)))
				return a.SaveView(nil)
			}

			var refs []app.ViewRef
			if len(pageIDs) > 0 {
				fmt.Fprintf(a.Out, " %s\n", th.Bold("Páginas"))
				for _, id := range pageIDs {
					page := g.pages.Pages[id]
					refs = append(refs, app.ViewRef{Kind: core.NodePage, ID: id})
					fmt.Fprintf(a.Out, " %d  %s  %s\n", len(refs), pageTitle(page), th.Dim(pageMeta(a, page, g.pageConns(id))))
					if s := snippet(th, page.Body, first); s != "" {
						fmt.Fprintf(a.Out, "      %s\n", s)
					}
				}
			}
			if len(cardIDs) > 0 {
				if len(pageIDs) > 0 {
					fmt.Fprintln(a.Out)
				}
				fmt.Fprintf(a.Out, " %s\n", th.Bold("Cartões"))
				for _, id := range cardIDs {
					card := g.leitner.Cards[id]
					refs = append(refs, app.ViewRef{Kind: core.NodeCard, ID: id})
					where := "sem deck"
					if deck, ok := g.decks.Decks[card.Deck()]; ok {
						where = "deck " + deck.Name
					}
					fmt.Fprintf(a.Out, " %d  %s  %s\n", len(refs), card.Front, th.Dim(where))
					if s := snippet(th, card.Back, first); s != "" {
						fmt.Fprintf(a.Out, "      %s\n", s)
					}
				}
			}
			return a.SaveView(refs)
		},
	}
}
