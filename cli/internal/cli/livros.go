// Livros (catálogo 1.7, spec §3 "Livros" e §4.16; spec/CLI.md §3 "Livros"):
// o livro cadastrado é identidade no log (título, autor, formato, sha256 do
// arquivo); o arquivo fica na pasta de livros do pnn (~/.pnn/livros/), fora
// do log, e viaja cifrado pelo bucket no `pnn sync` (spec §5, "Livros no
// bucket"). `pnn livro arquivo.epub` cadastra e guarda; `pnn livros` lista;
// `pnn ler N` abre (ler.go); `pnn anotacoes N` lista as anotações.
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/onofre-matheus/tcc/cli/core"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/epub"
	pnnsync "github.com/onofre-matheus/tcc/cli/internal/sync"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/onofre-matheus/tcc/cli/store"
	"github.com/spf13/cobra"
)

const (
	kindBook       = core.NodeBook
	kindAnnotation = "annotation"
)

// --- a pasta de livros ---

// booksDir é onde o pnn guarda os EPUBs, um por livro, pelo id. O usuário
// cadastra uma vez (de onde o arquivo estiver) e esquece: o original pode
// mudar de lugar ou sumir, e o `pnn sync` leva e traz a cópia cifrada.
func booksDir(a *app.App) string { return filepath.Join(a.Dir, "livros") }

func bookPath(a *app.App, id string) string { return pnnsync.LocalBookPath(booksDir(a), id) }

func hasBookFile(a *app.App, id string) bool {
	_, err := os.Stat(bookPath(a, id))
	return err == nil
}

// storeBookCopy copia o EPUB para a pasta de livros, em fluxo: temporário +
// rename (nada pela metade), pasta 0700, arquivo 0600.
func storeBookCopy(a *app.App, id, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	dir := booksDir(a)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, id+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), bookPath(a, id))
}

// bookFile: o EPUB do livro neste dispositivo, se já estiver guardado.
func bookFile(a *app.App, id string, query string) (string, error) {
	path := bookPath(a, id)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("o arquivo do livro ainda não está neste dispositivo — `pnn sync` traz do bucket (com PNN_LIVROS_SENHA), ou `pnn livro arquivo.epub` guarda de novo a partir do original")
	}
	return path, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checkBookFormat recusa cedo, com a saída, o que o pnn não lê.
func checkBookFormat(path string) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mobi", ".azw", ".azw3", ".kfx", ".prc":
		return fmt.Errorf("MOBI/AZW é formato fechado da Amazon e o pnn não lê — converta para EPUB com o Calibre: ebook-convert %q %q", filepath.Base(path), strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+".epub")
	case ".pdf":
		return fmt.Errorf("o pnn lê EPUB, não PDF: o texto de um PDF vem quebrado no terminal (cabeçalhos, hifenização, colunas) — se o livro existir em EPUB, use essa versão")
	}
	return nil
}

func openEPUB(path string) (*epub.Book, error) {
	if err := checkBookFormat(path); err != nil {
		return nil, err
	}
	book, err := epub.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return book, nil
}

// --- projeção e referência ---

func loadBooks(a *app.App) (core.BooksState, error) {
	events, err := a.Store.Events()
	if err != nil {
		return core.BooksState{}, err
	}
	return core.Books(events)
}

// findBook resolve "N" (última tela) ou parte do título, sem caixa nem
// acento, como findPage; várias candidatas viram lista numerada e id "".
func findBook(a *app.App, state core.BooksState, query string) (string, error) {
	if n, err := strconv.Atoi(query); err == nil {
		ref, err := a.ResolveView(n)
		if err != nil {
			return "", err
		}
		if _, ok := state.Books[ref.ID]; ref.Kind != kindBook || !ok {
			return "", fmt.Errorf("o item %d da última tela não é um livro — veja `pnn livros`", n)
		}
		return ref.ID, nil
	}
	words := strings.Fields(fold(query))
	if len(words) == 0 {
		return "", fmt.Errorf("informe o número ou parte do título do livro")
	}
	var found []string
	for _, id := range state.Order {
		title := fold(state.Books[id].Title)
		if title == strings.Join(words, " ") {
			return id, nil
		}
		if containsAll(title, words) {
			found = append(found, id)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("nenhum livro com %q no título — veja `pnn livros`", query)
	case 1:
		return found[0], nil
	}
	th := ui.Theme{On: a.Color}
	fmt.Fprintf(a.Out, "%s\n", th.Dim(fmt.Sprintf("%d livros casam com %q — escolha pelo número:", len(found), query)))
	return "", printBookRows(a, state, found)
}

// bookLabel: "Título — Autor".
func bookLabel(b core.Book) string {
	if b.Author == "" {
		return b.Title
	}
	return b.Title + " — " + b.Author
}

// bookMeta: "42% · 3 anotações · lido hoje · ⚡2 · ainda não baixado".
func bookMeta(a *app.App, b core.Book, hasFile bool, synapses int) string {
	var parts []string
	switch {
	case b.Archived:
		parts = append(parts, "arquivado")
	case b.Progress == nil:
		parts = append(parts, "não começado")
	case b.ProgressPercent != nil:
		parts = append(parts, fmt.Sprintf("%d%%", *b.ProgressPercent))
	}
	if n := len(b.Annotations); n > 0 {
		parts = append(parts, fmt.Sprintf("%d anotaç%s", n, map[bool]string{true: "ão", false: "ões"}[n == 1]))
	}
	if b.ProgressAt != nil {
		parts = append(parts, "lido "+relDay(a, *b.ProgressAt))
	}
	if synapses > 0 {
		parts = append(parts, fmt.Sprintf("⚡%d", synapses))
	}
	if !hasFile {
		parts = append(parts, "⚠ ainda não baixado — pnn sync")
	}
	return strings.Join(parts, " · ")
}

func printBookRows(a *app.App, state core.BooksState, ids []string) error {
	th := ui.Theme{On: a.Color}
	events, err := a.Store.Events()
	if err != nil {
		return err
	}
	links, err := core.Links(events)
	if err != nil {
		return err
	}
	refs := make([]app.ViewRef, len(ids))
	for i, id := range ids {
		b := state.Books[id]
		hasFile := hasBookFile(a, id)
		refs[i] = app.ViewRef{Kind: kindBook, ID: id}
		synapses := len(links.Neighbors[core.Node{Kind: core.NodeBook, ID: id}.Key()])
		fmt.Fprintf(a.Out, " %d  %s  %s\n", i+1, bookLabel(b), th.Dim(bookMeta(a, b, hasFile, synapses)))
	}
	return a.SaveView(refs)
}

// --- cadastro ---

// O que o cadastro fez com o arquivo.
const (
	bookNew      = "novo"       // cadastrado agora, cópia guardada
	bookRestored = "restaurado" // já cadastrado; a cópia deste dispositivo voltou
	bookPresent  = "presente"   // já cadastrado e já guardado
)

// registerBook cadastra o EPUB e guarda a cópia na pasta de livros. O mesmo
// arquivo (pelo sha256) não cadastra de novo: só garante a cópia local.
func registerBook(a *app.App, path, title, author string) (string, string, *epub.Book, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", nil, err
	}
	if err := checkBookFormat(abs); err != nil {
		return "", "", nil, err
	}
	sum, err := fileSHA256(abs)
	if err != nil {
		return "", "", nil, err
	}
	state, err := loadBooks(a)
	if err != nil {
		return "", "", nil, err
	}
	for _, id := range sortedBookIDs(state) {
		if state.Books[id].SHA256 != sum {
			continue
		}
		if hasBookFile(a, id) {
			return id, bookPresent, nil, nil
		}
		return id, bookRestored, nil, storeBookCopy(a, id, abs)
	}
	book, err := openEPUB(abs)
	if err != nil {
		return "", "", nil, err
	}
	title = strings.TrimSpace(cmpOrString(title, book.Title, strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))))
	author = strings.TrimSpace(cmpOrString(author, book.Author))
	id := store.UUIDv7()
	// A cópia antes do evento: cadastro sem arquivo guardado não fica no log.
	if err := storeBookCopy(a, id, abs); err != nil {
		return "", "", nil, err
	}
	payload := map[string]any{"book_id": id, "title": title, "format": "epub", "sha256": sum}
	if author != "" {
		payload["author"] = author
	}
	if _, err := a.Store.Append("book.registered", payload); err != nil {
		os.Remove(bookPath(a, id))
		return "", "", nil, err
	}
	return id, bookNew, book, nil
}

// syncHint: o que acontece com o arquivo no próximo sync.
func syncHint(a *app.App) string {
	if cfg, _, err := syncConfig(a); err != nil || cfg.BookPassphrase == "" {
		return "defina PNN_LIVROS_SENHA para o `pnn sync` levar o arquivo, cifrado, aos outros dispositivos"
	}
	return "sobe cifrado no próximo `pnn sync`"
}

// sortedBookIDs: os não arquivados primeiro (na ordem da lista), depois os
// arquivados — o mesmo arquivo cadastrado duas vezes religa o que está em uso.
func sortedBookIDs(state core.BooksState) []string {
	ids := append([]string(nil), state.Order...)
	for id, b := range state.Books {
		if b.Archived {
			ids = append(ids, id)
		}
	}
	return ids
}

func cmpOrString(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func chapterSummary(book *epub.Book) string {
	s := fmt.Sprintf("%d capítulos", len(book.Chapters))
	if book.HasPages {
		s += ", com a paginação impressa"
	}
	return s
}

func newLivroCmd(a *app.App) *cobra.Command {
	var title, author string
	cmd := &cobra.Command{
		Use:   "livro arquivo.epub",
		Short: "cadastra um livro EPUB (book.registered) e guarda uma cópia — o `pnn sync` a leva cifrada aos outros dispositivos",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id, outcome, book, err := registerBook(a, args[0], title, author)
			if err != nil {
				return err
			}
			state, err := loadBooks(a)
			if err != nil {
				return err
			}
			b := state.Books[id]
			th := ui.Theme{On: a.Color}
			switch outcome {
			case bookNew:
				fmt.Fprintf(a.Out, "📖 cadastrado: %s %s\n", bookLabel(b), th.Dim("("+chapterSummary(book)+")"))
				fmt.Fprintf(a.Out, "%s\n", th.Dim("guardado no pnn — o original pode mudar de lugar; "+syncHint(a)))
			case bookRestored:
				fmt.Fprintf(a.Out, "📖 já cadastrado: %s — o arquivo foi guardado de novo neste dispositivo\n", bookLabel(b))
			default:
				fmt.Fprintf(a.Out, "📖 já cadastrado: %s — o arquivo já está guardado\n", bookLabel(b))
			}
			if b.Archived {
				fmt.Fprintln(a.Out, th.Dim("(está arquivado — `pnn livros --arquivados`)"))
			}
			if err := a.SaveView([]app.ViewRef{{Kind: kindBook, ID: id}}); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "ler: %s\n", th.Blue("pnn ler 1"))
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "titulo", "", "título (padrão: o do EPUB)")
	cmd.Flags().StringVar(&author, "autor", "", "autor (padrão: o do EPUB)")
	return cmd
}

// --- lista ---

func newLivrosCmd(a *app.App) *cobra.Command {
	var asJSON, archived bool
	cmd := &cobra.Command{
		Use:   "livros",
		Short: "lista os livros: progresso, anotações e se o arquivo está neste dispositivo (--arquivados)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			state, err := loadBooks(a)
			if err != nil {
				return err
			}
			if asJSON {
				raw, err := json.MarshalIndent(state, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(a.Out, string(raw))
				return nil
			}
			th := ui.Theme{On: a.Color}
			ids := state.Order
			if archived {
				ids = nil
				for _, id := range sortedBookIDs(state) {
					if state.Books[id].Archived {
						ids = append(ids, id)
					}
				}
			}
			if len(ids) == 0 {
				if archived {
					fmt.Fprintln(a.Out, "nenhum livro arquivado")
					return nil
				}
				fmt.Fprintf(a.Out, "nenhum livro — %s cadastra um; %s cadastra uma pasta\n",
					th.Blue("pnn livro arquivo.epub"), th.Blue("pnn livros procurar ~/Livros --cadastrar"))
				return nil
			}
			fmt.Fprintln(a.Out, th.Title("📖 Livros"))
			if err := printBookRows(a, state, ids); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "\n%s\n", th.Dim("pnn ler N abre · pnn anotacoes N lista as anotações · pnn livro arquivo.epub cadastra"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite a projeção books em JSON")
	cmd.Flags().BoolVar(&archived, "arquivados", false, "lista os arquivados")
	cmd.AddCommand(newProcurarCmd(a))
	return cmd
}

// newProcurarCmd varre uma pasta atrás de EPUBs: guarda de novo, pelo
// sha256, os já cadastrados que faltam neste dispositivo (sem bucket, sem a
// senha dos livros) e, com --cadastrar, cadastra os novos.
func newProcurarCmd(a *app.App) *cobra.Command {
	var register bool
	cmd := &cobra.Command{
		Use:   "procurar pasta",
		Short: "procura EPUBs na pasta: guarda de novo os cadastrados que faltam aqui (--cadastrar cadastra os novos)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			var files []string
			err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".epub") {
					files = append(files, path)
				}
				return nil
			})
			if err != nil {
				return err
			}
			state, err := loadBooks(a)
			if err != nil {
				return err
			}
			bySum := map[string]string{}
			ids := sortedBookIDs(state)
			for i := len(ids) - 1; i >= 0; i-- { // o primeiro da lista vence
				bySum[state.Books[ids[i]].SHA256] = ids[i]
			}
			th := ui.Theme{On: a.Color}
			var restored, unknown []string
			same := 0
			for _, file := range files {
				sum, err := fileSHA256(file)
				if err != nil {
					fmt.Fprintln(a.Out, th.Warn("⚠ "+err.Error()))
					continue
				}
				id, ok := bySum[sum]
				switch {
				case !ok:
					unknown = append(unknown, file)
				case hasBookFile(a, id) || slices.Contains(restored, id):
					same++
				default:
					if err := storeBookCopy(a, id, file); err != nil {
						fmt.Fprintln(a.Out, th.Warn("⚠ "+err.Error()))
						continue
					}
					restored = append(restored, id)
				}
			}
			fmt.Fprintf(a.Out, "%d EPUB(s) em %s\n", len(files), root)
			for _, id := range restored {
				fmt.Fprintf(a.Out, " ✔ guardado de novo: %s\n", bookLabel(state.Books[id]))
			}
			if same > 0 {
				fmt.Fprintf(a.Out, " %s\n", th.Dim(fmt.Sprintf("%d já guardado(s) neste dispositivo", same)))
			}
			if len(unknown) == 0 {
				return nil
			}
			if !register {
				fmt.Fprintf(a.Out, " %d não cadastrado(s):\n", len(unknown))
				for i, file := range unknown {
					if i == 10 {
						fmt.Fprintf(a.Out, "   … e mais %d\n", len(unknown)-10)
						break
					}
					fmt.Fprintf(a.Out, "   %s\n", filepath.Base(file))
				}
				fmt.Fprintf(a.Out, " %s\n", th.Dim("cadastrar todos: pnn livros procurar "+args[0]+" --cadastrar · um: pnn livro arquivo.epub"))
				return nil
			}
			for _, file := range unknown {
				id, _, _, err := registerBook(a, file, "", "")
				if err != nil {
					fmt.Fprintln(a.Out, th.Warn(" ⚠ "+err.Error()))
					continue
				}
				state, err := loadBooks(a)
				if err != nil {
					return err
				}
				fmt.Fprintf(a.Out, " 📖 cadastrado: %s\n", bookLabel(state.Books[id]))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&register, "cadastrar", false, "cadastra os EPUBs que ainda não estão no log")
	return cmd
}

// --- anotações ---

func newAnotacoesCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "anotacoes N|título",
		Short: "lista as anotações do livro em ordem de leitura — numeradas para `pnn editar` e `pnn apagar`",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			state, err := loadBooks(a)
			if err != nil {
				return err
			}
			query := strings.Join(args, " ")
			id, err := findBook(a, state, query)
			if err != nil || id == "" {
				return err
			}
			b := state.Books[id]
			th := ui.Theme{On: a.Color}
			if len(b.Annotations) == 0 {
				fmt.Fprintf(a.Out, "nenhuma anotação em %s — abra com %s e use /nota ou /grifo\n", b.Title, th.Blue("pnn ler "+query))
				return nil
			}
			// Com o arquivo aqui, os capítulos ganham número e título como na
			// leitura; sem ele, a posição do spine basta.
			chapters := map[int]string{}
			if path, err := bookFile(a, id, query); err == nil {
				if book, err := epub.Open(path); err == nil {
					for i, ch := range book.Chapters {
						label := fmt.Sprintf("cap. %d", i+1)
						if ch.Title != "" {
							label += " · " + ch.Title
						}
						chapters[ch.Spine] = label
					}
				}
			}
			fmt.Fprintf(a.Out, "%s %s\n", th.Title("📖 "+bookLabel(b)), th.Dim(fmt.Sprintf("· %d anotaç%s", len(b.Annotations), map[bool]string{true: "ão", false: "ões"}[len(b.Annotations) == 1])))
			refs := make([]app.ViewRef, 0, len(b.Annotations))
			lastChapter := 0
			for i, annID := range b.Annotations {
				an := state.Annotations[annID]
				if an.Loc.Chapter != lastChapter {
					lastChapter = an.Loc.Chapter
					label, ok := chapters[an.Loc.Chapter]
					if !ok {
						label = fmt.Sprintf("seção %d do EPUB", an.Loc.Chapter)
					}
					fmt.Fprintf(a.Out, "\n%s\n", th.Bold(label))
				}
				mark := "✎"
				if an.Text == "" {
					mark = "▍"
				}
				page := ""
				if an.Loc.Page != "" {
					page = " " + th.Dim("(p. "+an.Loc.Page+")")
				}
				fmt.Fprintf(a.Out, " %d  %s %q%s\n", i+1, mark, an.Quote, page)
				if an.Text != "" {
					fmt.Fprintf(a.Out, "      %s\n", an.Text)
				}
				refs = append(refs, app.ViewRef{Kind: kindAnnotation, ID: annID})
			}
			fmt.Fprintf(a.Out, "\n%s\n", th.Dim("pnn editar N \"novo comentário\" · pnn apagar N"))
			return a.SaveView(refs)
		},
	}
}

// --- edição pelos comandos genéricos (editar, apagar, arquivar) ---

func editarLivro(a *app.App, id, title string) error {
	state, err := loadBooks(a)
	if err != nil {
		return err
	}
	b, ok := state.Books[id]
	if !ok {
		return fmt.Errorf("o livro não existe mais — rode `pnn livros`")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf(`informe o novo título: pnn editar N "Título"`)
	}
	if _, err := a.Store.Append("book.edited", map[string]any{"book_id": id, "title": title}); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "✔ livro renomeado: %s → %s\n", b.Title, title)
	return nil
}

func editarAnotacao(a *app.App, id, text string) error {
	state, err := loadBooks(a)
	if err != nil {
		return err
	}
	if _, ok := state.Annotations[id]; !ok {
		return fmt.Errorf("a anotação não existe mais — rode `pnn anotacoes`")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf(`informe o novo comentário: pnn editar N "comentário"`)
	}
	if _, err := a.Store.Append("annotation.edited", map[string]any{"annotation_id": id, "text": text}); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "✎ anotação editada: %s\n", text)
	return nil
}

func apagarAnotacao(a *app.App, id string) error {
	state, err := loadBooks(a)
	if err != nil {
		return err
	}
	an, ok := state.Annotations[id]
	if !ok {
		return fmt.Errorf("a anotação não existe mais — rode `pnn anotacoes`")
	}
	if _, err := a.Store.Append("annotation.deleted", map[string]any{"annotation_id": id}); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "✗ anotação apagada: %q\n", an.Quote)
	return nil
}

func arquivarLivro(a *app.App, id string) error {
	state, err := loadBooks(a)
	if err != nil {
		return err
	}
	b, ok := state.Books[id]
	if !ok {
		return fmt.Errorf("o livro não existe mais — rode `pnn livros`")
	}
	if _, err := a.Store.Append("book.archived", map[string]any{"book_id": id}); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "✔ livro arquivado: %s (fora da lista, anotações preservadas — `pnn livros --arquivados`)\n", b.Title)
	return nil
}
