// `pnn config` — as variáveis de ambiente que o pnn lê, o valor de cada uma
// neste terminal (ou o que vale quando está vazia) e para que serve. A
// tabela abaixo é a fonte da verdade: um teste varre o código e falha se o
// pnn passar a ler uma variável que não está aqui.
//
// Segredo nunca aparece — nem na tela, nem no --json: só "definida". A
// configuração vem do ambiente (e as credenciais AWS, da cadeia do SDK) —
// ou, com `pnn login`, do login.json da réplica até o logout (login.go); a
// primeira linha diz qual vale.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/notify"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type envVar struct {
	name    string
	group   string
	purpose string
	secret  bool
	// extension: o campo equivalente no painel de sync da extensão (o
	// navegador não tem ambiente).
	extension string
	// effective: o que o pnn usa de fato agora (com ou sem a variável);
	// nil = o próprio valor, ou nada.
	effective func(a *app.App) string
}

const (
	groupReplica = "Réplica e fuso"
	groupSync    = "Sincronização (pnn sync)"
	groupAWS     = "Credenciais AWS (lidas pelo SDK da AWS; também vêm de ~/.aws)"
	groupEditor  = "Editor e leitura de páginas"
	groupOutput  = "Saída e avisos"
)

var configGroups = []string{groupReplica, groupSync, groupAWS, groupEditor, groupOutput}

var envVars = []envVar{
	{name: "PNN_HOME", group: groupReplica,
		purpose:   "onde fica a réplica local: o log de eventos, a pasta de livros, a última tela",
		effective: func(a *app.App) string { return a.Dir }},
	{name: "PNN_TZ", group: groupReplica,
		purpose:   "fuso IANA de \"hoje\" (dia, streak, semana, agenda, lembretes); vence o TZ",
		effective: func(a *app.App) string { return a.TZ + " (" + tzSource() + ")" }},
	{name: "TZ", group: groupReplica,
		purpose: "fuso do sistema; vale só se for nome IANA (com /) e não houver PNN_TZ"},

	{name: "PNN_S3_BUCKET", group: groupSync, extension: "bucket",
		purpose: "o bucket S3 do sync — sem ele, `pnn sync` não roda"},
	{name: "PNN_S3_PREFIX", group: groupSync, extension: "prefix",
		purpose:   "prefixo das chaves no bucket (um bucket pode servir a mais de uma coisa)",
		effective: func(*app.App) string { return orDefault("PNN_S3_PREFIX", "pnn/") }},
	{name: "PNN_S3_ENDPOINT", group: groupSync, extension: "endpoint",
		purpose:   "endpoint de serviço compatível com S3 (R2, MinIO, Backblaze)",
		effective: func(*app.App) string { return orDefault("PNN_S3_ENDPOINT", "AWS") }},
	{name: "PNN_S3_REGION", group: groupSync, extension: "region",
		purpose:   "região do bucket",
		effective: func(*app.App) string { return orDefault("PNN_S3_REGION", "a da cadeia da AWS (AWS_REGION…)") }},
	{name: "PNN_LIVROS_SENHA", group: groupSync, extension: "bookPassphrase", secret: true,
		purpose: "senha dos livros: cifra os EPUBs no bucket — a mesma do painel da extensão; sem ela, os livros não sincronizam"},

	{name: "AWS_ACCESS_KEY_ID", group: groupAWS, extension: "accessKeyId", secret: true,
		purpose: "chave de acesso do bucket"},
	{name: "AWS_SECRET_ACCESS_KEY", group: groupAWS, extension: "secretAccessKey", secret: true,
		purpose: "segredo da chave de acesso"},
	{name: "AWS_SESSION_TOKEN", group: groupAWS, extension: "sessionToken", secret: true,
		purpose: "token de credencial temporária (STS)"},
	{name: "AWS_PROFILE", group: groupAWS,
		purpose:   "perfil de ~/.aws/credentials a usar",
		effective: func(*app.App) string { return orDefault("AWS_PROFILE", "default") }},
	{name: "AWS_REGION", group: groupAWS,
		purpose: "região, quando não há PNN_S3_REGION"},
	{name: "AWS_DEFAULT_REGION", group: groupAWS,
		purpose: "região (nome antigo), quando não há AWS_REGION"},

	{name: "PNN_EDITOR", group: groupEditor,
		purpose: "editor das páginas (pnn p, pnn pagina -e, Ctrl+E no foco); vence VISUAL e EDITOR; passa pelo shell (\"code --wait\" funciona)",
		effective: func(*app.App) string {
			if editor := resolveEditor(); editor != "" {
				return editor
			}
			return "nenhum encontrado"
		}},
	{name: "VISUAL", group: groupEditor,
		purpose: "editor do sistema, quando não há PNN_EDITOR"},
	{name: "EDITOR", group: groupEditor,
		purpose: "editor do sistema, quando não há PNN_EDITOR nem VISUAL (senão: editor, nano ou vi)"},
	{name: "PNN_PAGER", group: groupEditor,
		purpose:   "paginador da leitura de páginas (pnn pagina); vence PAGER; \"cat\" desliga",
		effective: func(*app.App) string { return resolvePager() }},
	{name: "PAGER", group: groupEditor,
		purpose: "paginador do sistema, quando não há PNN_PAGER"},
	{name: "LESS", group: groupEditor,
		purpose:   "opções do less",
		effective: func(*app.App) string { return orDefault("LESS", "FRX (texto curto sai direto, como no git)") }},
	{name: "PNN_TEMA", group: groupEditor,
		purpose: "tema da página formatada: claro ou escuro",
		effective: func(*app.App) string {
			if lightBackground() {
				return "claro"
			}
			return "escuro"
		}},
	{name: "COLORFGBG", group: groupEditor,
		purpose: "cores do terminal, que alguns exportam: o fundo decide o tema quando não há PNN_TEMA"},

	{name: "NO_COLOR", group: groupOutput,
		purpose: "qualquer valor desliga as cores (a cor nunca é o único sinal)",
		effective: func(a *app.App) string {
			if a.Color {
				return "cores ligadas"
			}
			return "cores desligadas (NO_COLOR ou fora do terminal)"
		}},
	{name: "PNN_SILENCIO", group: groupOutput,
		purpose: "qualquer valor desliga som e notificações de desktop",
		effective: func(*app.App) string {
			if notify.Silenced() {
				return "notificações desligadas"
			}
			return "notificações ligadas"
		}},
	{name: "WSL_DISTRO_NAME", group: groupOutput,
		purpose: "o WSL define sozinho: as notificações também saem como aviso do Windows"},
}

func orDefault(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

// tzSource diz de onde veio o fuso (a mesma ordem de app.detectTZ).
func tzSource() string {
	switch {
	case os.Getenv("PNN_TZ") != "":
		return "PNN_TZ"
	case strings.Contains(os.Getenv("TZ"), "/"):
		return "TZ"
	}
	return "do sistema"
}

// configEntry é uma linha do --json.
type configEntry struct {
	Name      string  `json:"name"`
	Group     string  `json:"group"`
	Purpose   string  `json:"purpose"`
	Set       bool    `json:"set"`
	Secret    bool    `json:"secret"`
	Value     *string `json:"value"` // nulo se vazia ou segredo
	Effective string  `json:"effective,omitempty"`
	Extension string  `json:"extension,omitempty"`
}

func configEntries(a *app.App) []configEntry {
	entries := make([]configEntry, 0, len(envVars))
	for _, v := range envVars {
		value, set := os.LookupEnv(v.name)
		set = set && strings.TrimSpace(value) != ""
		e := configEntry{Name: v.name, Group: v.group, Purpose: v.purpose, Set: set, Secret: v.secret, Extension: v.extension}
		if set && !v.secret {
			e.Value = &value
		}
		if v.effective != nil {
			e.Effective = v.effective(a)
		}
		entries = append(entries, e)
	}
	return entries
}

func newConfigCmd(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"configuracao", "variaveis"},
		Short:   "lista as variáveis de configuração: o valor neste terminal e para que serve cada uma (segredos nunca aparecem)",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			entries := configEntries(a)
			if asJSON {
				raw, err := json.MarshalIndent(entries, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(a.Out, string(raw))
				return nil
			}
			th := ui.Theme{On: a.Color}
			width := 80
			if stdoutTerminal(a) {
				if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
					width = min(w, 100)
				}
			}
			fmt.Fprintf(a.Out, "%s %s\n", th.Title("⚙ Configuração"), th.Dim("— variáveis de ambiente"))
			for _, line := range wrapWords(loginStatus(a), width-2) {
				fmt.Fprintf(a.Out, " %s\n", line)
			}
			for _, group := range configGroups {
				fmt.Fprintf(a.Out, "\n%s\n", th.Bold(group))
				for _, e := range entries {
					if e.Group != group {
						continue
					}
					fmt.Fprintf(a.Out, " %-21s %s\n", e.Name, configState(th, e, width))
					purpose := e.Purpose
					if e.Extension != "" {
						purpose += " · na extensão: " + e.Extension
					}
					for _, line := range wrapWords(purpose, width-24) {
						fmt.Fprintf(a.Out, " %-21s %s\n", "", th.Dim(line))
					}
				}
			}
			fmt.Fprintf(a.Out, "\n%s\n", th.Dim("defina no perfil do shell: export NOME=valor no ~/.bashrc"))
			fmt.Fprintf(a.Out, "%s\n", th.Dim("em JSON: pnn config --json"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emite a lista em JSON (segredos sem valor)")
	return cmd
}

// wrapWords quebra o texto em linhas de até width colunas, nas palavras.
func wrapWords(text string, width int) []string {
	width = max(width, 20)
	var lines []string
	var line []string
	size := 0
	for _, word := range strings.Fields(text) {
		w := ansi.StringWidth(word)
		if size > 0 && size+1+w > width {
			lines = append(lines, strings.Join(line, " "))
			line, size = nil, 0
		}
		if size > 0 {
			size++
		}
		line, size = append(line, word), size+w
	}
	if len(line) > 0 {
		lines = append(lines, strings.Join(line, " "))
	}
	return lines
}

// configState: o valor (ou "definida", se segredo), e o que vale de fato.
func configState(th ui.Theme, e configEntry, width int) string {
	var state string
	switch {
	case e.Set && e.Secret:
		state = th.Green("definida") + th.Dim(" (segredo — o valor não aparece)")
	case e.Set:
		state = th.Green(truncate(*e.Value, max(20, width-25)))
	default:
		state = th.Dim("—")
	}
	if e.Effective != "" && (e.Value == nil || e.Effective != *e.Value) {
		// "—" ou o valor curto, mais "  → ": o resto da linha é do efetivo
		used := 23 + ansi.StringWidth(ansi.Strip(state)) + 4
		state += th.Dim("  → " + truncate(e.Effective, max(12, width-used-1)))
	}
	return state
}
