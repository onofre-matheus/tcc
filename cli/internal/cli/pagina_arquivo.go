// O arquivo que o editor abre para uma página (catálogo 1.6): só o corpo, em
// markdown puro. Título, tags e conexões são metadado — mudam por comando
// (`pnn editar N "título"`, `--tag`, `pnn ligar`) e a página só os mostra.
// Nada do que foi escrito se perde: em qualquer erro o arquivo temporário
// fica, e o caminho vai na mensagem.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"golang.org/x/term"
)

// bodyFile é o conteúdo do temporário: o corpo e um \n final, como todo
// editor espera.
func bodyFile(body string) string {
	if body = cleanBody(body); body != "" {
		return body + "\n"
	}
	return ""
}

// cleanBody normaliza o que o editor devolve: CRLF vira LF, sai o BOM, as
// linhas em branco do começo e os espaços do fim (o \n final do editor não é
// conteúdo). A indentação da primeira linha fica, e um corpo que começa com
// "---" é guardado como está.
func cleanBody(s string) string {
	s = strings.TrimPrefix(strings.ReplaceAll(s, "\r\n", "\n"), "\ufeff")
	s = strings.TrimRight(s, " \t\r\n")
	for {
		line, rest, ok := strings.Cut(s, "\n")
		if !ok || strings.TrimSpace(line) != "" {
			break
		}
		s = rest
	}
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s
}

func hashTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return "#" + strings.Join(tags, " #")
}

// --- dobra de caixa e acento (busca, título) ---

var accentFold = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n',
}

// foldRunes passa a minúsculas e tira o acento, runa por runa — o índice de
// uma runa dobrada é o mesmo no texto original, o que permite destacar o
// trecho encontrado sem perder o acento.
func foldRunes(s string) []rune {
	rs := []rune(s)
	for i, r := range rs {
		r = unicode.ToLower(r)
		if base, ok := accentFold[r]; ok {
			r = base
		}
		rs[i] = r
	}
	return rs
}

func fold(s string) string { return string(foldRunes(s)) }

// --- editor, formatação e paginação ---

// editInEditor grava o conteúdo num .md temporário (0600), abre o editor e
// devolve o que ficou. Quem chama apaga o arquivo depois de gravar no log;
// em qualquer erro o arquivo fica e o caminho vai na mensagem.
func editInEditor(a *app.App, name, content string) (edited, path string, err error) {
	f, err := os.CreateTemp("", "pnn-"+slug(name)+"-*.md")
	if err != nil {
		return "", "", err
	}
	path = f.Name()
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(path)
		return "", "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", "", err
	}
	open := a.Editor
	if open == nil {
		open = launchEditor
	}
	if err := open(path); err != nil {
		return "", path, fmt.Errorf("o editor saiu com erro (%v) — nada gravado; o texto ficou em %s", err, path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", path, err
	}
	return string(raw), path, nil
}

// keptAt acrescenta ao erro o caminho do texto preservado.
func keptAt(err error, path string) error {
	return fmt.Errorf("%v — nada gravado; o texto ficou em %s", err, path)
}

// launchEditor abre $PNN_EDITOR, $VISUAL ou $EDITOR (nessa ordem; senão
// editor, nano ou vi) e espera fechar. Como no git, o valor passa pelo
// shell, então "code --wait" funciona.
func launchEditor(path string) error {
	editor := firstEnv("PNN_EDITOR", "VISUAL", "EDITOR")
	if editor == "" {
		for _, candidate := range []string{"editor", "nano", "vi"} {
			if _, err := exec.LookPath(candidate); err == nil {
				editor = candidate
				break
			}
		}
	}
	if editor == "" {
		return errors.New("nenhum editor encontrado — defina $EDITOR (ex.: export EDITOR=nano)")
	}
	cmd := exec.Command("sh", "-c", editor+` "$@"`, editor, path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// slug dá ao arquivo temporário um nome legível na barra do editor.
func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range foldRunes(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 32 {
			break
		}
	}
	if s := strings.Trim(b.String(), "-"); s != "" {
		return s
	}
	return "pagina"
}

// stdoutTerminal: a saída do App é o terminal de verdade (não pipe, não teste).
func stdoutTerminal(a *app.App) bool {
	f, ok := a.Out.(*os.File)
	return ok && f == os.Stdout && term.IsTerminal(int(os.Stdout.Fd()))
}

// renderMarkdown formata o corpo com o glamour: tema escuro ou claro (ver
// lightBackground); sem cor (NO_COLOR, pipe), o estilo ASCII sem o
// preenchimento de espaços no fim das linhas. Se falhar, o markdown cru é
// melhor que nada.
func renderMarkdown(a *app.App, md string) string {
	width := 80
	if stdoutTerminal(a) {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			width = min(w, 100)
		}
	}
	style := styles.NoTTYStyle
	if a.Color {
		style = styles.DarkStyle
		if lightBackground() {
			style = styles.LightStyle
		}
	}
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(width-4))
	if err != nil {
		return md + "\n"
	}
	out, err := r.Render(md)
	if err != nil {
		return md + "\n"
	}
	if !a.Color {
		lines := strings.Split(out, "\n")
		for i, line := range lines {
			lines[i] = strings.TrimRight(line, " ")
		}
		out = strings.Join(lines, "\n")
	}
	return out
}

// lightBackground decide o tema sem perguntar ao terminal: a consulta de cor
// de fundo (OSC 11, o "auto" do glamour) espera 5 s em cada terminal que não
// responde (tmux, screen, script). Vale $PNN_TEMA=claro|escuro; senão o
// $COLORFGBG que alguns terminais exportam ("15;0" = texto claro em fundo
// escuro); senão escuro, o mais comum em terminal.
func lightBackground() bool {
	switch fold(firstEnv("PNN_TEMA")) {
	case "claro", "light":
		return true
	case "escuro", "dark":
		return false
	}
	if v := os.Getenv("COLORFGBG"); v != "" {
		parts := strings.Split(v, ";")
		switch parts[len(parts)-1] {
		case "7", "15":
			return true
		}
	}
	return false
}

// pageOut escreve no terminal através do paginador ($PNN_PAGER, $PAGER ou
// less, com LESS=FRX como o git: texto curto sai direto); fora do terminal,
// escreve direto.
func pageOut(a *app.App, text string) error {
	pager := firstEnv("PNN_PAGER", "PAGER")
	if pager == "" {
		pager = "less"
	}
	if !stdoutTerminal(a) || pager == "cat" {
		_, err := io.WriteString(a.Out, text)
		return err
	}
	cmd := exec.Command("sh", "-c", pager)
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.Env = os.Environ()
	if os.Getenv("LESS") == "" {
		cmd.Env = append(cmd.Env, "LESS=FRX")
	}
	if err := cmd.Run(); err != nil { // paginador ausente: escreve direto
		_, werr := io.WriteString(a.Out, text)
		return werr
	}
	return nil
}
