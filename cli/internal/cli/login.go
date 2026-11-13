// `pnn login` e `pnn logout` (spec/SPEC.md §5, "Login e logout de um
// dispositivo"): o pnn como sessão de um aparelho. No fim do expediente, o
// logout sincroniza, CONFERE que tudo o que este aparelho emitiu está no
// bucket e só então apaga a réplica, os livros e a credencial; em qualquer
// outro aparelho, o login pede a credencial uma vez, restaura tudo e você
// continua de onde parou. O bucket é um só — não há ambientes.
//
// A credencial do login fica em login.json (0600) só enquanto durar a
// sessão: sem login, vale o ambiente do shell (PNN_S3_*, cadeia da AWS), que
// o pnn não consegue apagar — por isso, no aparelho de trabalho, entra-se
// por aqui.
//
// A trava réplica ↔ bucket (o arquivo `remoto`) vale com ou sem login: a
// réplica que já sincronizou com um bucket recusa falar com outro, porque o
// sync sobe tudo o que o dispositivo emitiu — trocar o bucket por baixo
// levaria os eventos de um para o outro.
package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/sync"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	loginFile  = "login.json"
	remoteFile = "remoto"
)

// machineIDPath: a identidade da máquina (os testes trocam).
var machineIDPath = "/etc/machine-id"

type loginData struct {
	Bucket          string `json:"bucket"`
	Prefix          string `json:"prefix"`
	Endpoint        string `json:"endpoint,omitempty"`
	Region          string `json:"region,omitempty"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token,omitempty"`
	BookPassphrase  string `json:"book_passphrase,omitempty"`
	Since           string `json:"since"`
}

func (d loginData) config() sync.Config {
	return sync.Config{
		Bucket: d.Bucket, Prefix: d.Prefix, Endpoint: d.Endpoint, Region: d.Region,
		BookPassphrase: d.BookPassphrase,
		AccessKeyID:    d.AccessKeyID, SecretAccessKey: d.SecretAccessKey, SessionToken: d.SessionToken,
	}
}

func loadLogin(a *app.App) (*loginData, error) {
	raw, err := os.ReadFile(filepath.Join(a.Dir, loginFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d loginData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("%s ilegível (%v) — `pnn logout` e entre de novo", loginFile, err)
	}
	return &d, nil
}

// writePrivate grava num temporário 0600 e renomeia: nada pela metade, e
// nunca legível por outro usuário nem por um instante.
func writePrivate(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// syncConfig: a configuração do sync — a do login, se houver; senão a do
// ambiente. fromLogin diz qual.
func syncConfig(a *app.App) (cfg sync.Config, fromLogin bool, err error) {
	login, err := loadLogin(a)
	if err != nil {
		return sync.Config{}, false, err
	}
	if login != nil {
		return login.config(), true, nil
	}
	cfg, err = sync.ConfigFromEnv()
	return cfg, false, err
}

// --- a trava réplica ↔ bucket ---

func readRemote(a *app.App) string {
	raw, err := os.ReadFile(filepath.Join(a.Dir, remoteFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func checkRemote(a *app.App, cfg sync.Config) error {
	if bound := readRemote(a); bound != "" && bound != cfg.Remote() {
		return fmt.Errorf("esta réplica pertence a %s, e a configuração aponta para %s — o sync levaria os eventos de um bucket para o outro; para trocar de bucket, `pnn logout` antes", bound, cfg.Remote())
	}
	return nil
}

func bindRemote(a *app.App, cfg sync.Config) error {
	if readRemote(a) == cfg.Remote() {
		return nil
	}
	return os.WriteFile(filepath.Join(a.Dir, remoteFile), []byte(cfg.Remote()+"\n"), 0o644)
}

// machineDeviceID: o id do dispositivo derivado da máquina e da pasta da
// réplica — o mesmo a cada login nesta pasta desta máquina (o logout e o
// login reusam o mesmo objeto do bucket, em vez de um objeto novo por dia),
// e outro em outra pasta (duas réplicas nunca dividem um objeto). "" sem a
// identidade da máquina: aí o id é aleatório, como sempre foi.
func machineDeviceID(dir string) string {
	raw, err := os.ReadFile(machineIDPath)
	id := strings.TrimSpace(string(raw))
	if err != nil || id == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	sum := sha256.Sum256([]byte("pnn:" + id + ":" + abs))
	return "dev-" + hex.EncodeToString(sum[:])[:32]
}

// --- perguntas ---

type prompter struct {
	a *app.App
	r *bufio.Reader
}

func newPrompter(a *app.App) prompter {
	in := a.In
	if in == nil {
		in = strings.NewReader("")
	}
	return prompter{a: a, r: bufio.NewReader(in)}
}

func (p prompter) readLine() (string, error) {
	line, err := p.r.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", errors.New("cancelado (sem resposta)")
	}
	return strings.TrimSpace(line), nil
}

// line pergunta; Enter aceita o padrão.
func (p prompter) line(label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(p.a.Out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(p.a.Out, "%s: ", label)
	}
	v, err := p.readLine()
	if err != nil {
		return "", err
	}
	if v == "" {
		return def, nil
	}
	return v, nil
}

// secret pergunta sem eco no terminal; Enter mantém o padrão (que nunca é
// mostrado).
func (p prompter) secret(label, def string) (string, error) {
	hint := ""
	if def != "" {
		hint = " [Enter mantém a do ambiente]"
	}
	fmt.Fprintf(p.a.Out, "%s%s: ", label, hint)
	var v string
	if f, ok := p.a.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		raw, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(p.a.Out)
		if err != nil {
			return "", err
		}
		v = strings.TrimSpace(string(raw))
	} else {
		var err error
		if v, err = p.readLine(); err != nil {
			return "", err
		}
	}
	if v == "" {
		return def, nil
	}
	return v, nil
}

func (p prompter) confirm(question string) bool {
	fmt.Fprintf(p.a.Out, "%s [s/N]: ", question)
	v, err := p.readLine()
	return err == nil && (strings.EqualFold(v, "s") || strings.EqualFold(v, "sim"))
}

// --- login ---

type bucketOpener func(context.Context, sync.Config) (sync.Bucket, error)

func newLoginCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "entra neste dispositivo: pede a credencial do bucket uma vez, restaura tudo e você continua de onde parou",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return loginWith(cmd.Context(), a, sync.NewBucket)
		},
	}
}

func loginWith(ctx context.Context, a *app.App, open bucketOpener) error {
	th := ui.Theme{On: a.Color}
	if login, err := loadLogin(a); err != nil {
		return err
	} else if login != nil {
		fmt.Fprintf(a.Out, "🔓 já conectado a %s desde %s — `pnn logout` para sair\n", login.config().Remote(), login.Since)
		return nil
	}

	env, _ := sync.ConfigFromEnv() // os padrões das perguntas, se houver
	p := newPrompter(a)
	fmt.Fprintln(a.Out, th.Title("🔑 Login")+" "+th.Dim("— a credencial fica neste dispositivo só até o `pnn logout`"))
	d := loginData{}
	var err error
	if d.Bucket, err = p.line("Bucket", env.Bucket); err != nil {
		return err
	}
	if d.Endpoint, err = p.line("Endpoint (vazio = AWS)", env.Endpoint); err != nil {
		return err
	}
	if d.Region, err = p.line("Região (vazio = padrão)", env.Region); err != nil {
		return err
	}
	if d.Prefix, err = p.line("Prefixo", cmpOrString(os.Getenv("PNN_S3_PREFIX"), "pnn/")); err != nil {
		return err
	}
	if d.AccessKeyID, err = p.secret("Access key ID", os.Getenv("AWS_ACCESS_KEY_ID")); err != nil {
		return err
	}
	if d.SecretAccessKey, err = p.secret("Secret access key", os.Getenv("AWS_SECRET_ACCESS_KEY")); err != nil {
		return err
	}
	if d.BookPassphrase, err = p.secret("Senha dos livros (Enter pula)", strings.TrimSpace(os.Getenv("PNN_LIVROS_SENHA"))); err != nil {
		return err
	}
	if d.Bucket == "" || d.AccessKeyID == "" || d.SecretAccessKey == "" {
		return errors.New("login precisa de bucket, access key e secret")
	}
	if !strings.HasSuffix(d.Prefix, "/") {
		d.Prefix += "/"
	}
	cfg := d.config()

	// A conta funciona? (credencial errada, bucket inexistente, endpoint)
	bucket, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	if _, err := bucket.List(ctx, cfg.Prefix); err != nil {
		return fmt.Errorf("não consegui listar o bucket com essa credencial: %w", err)
	}

	// A réplica precisa ser desta conta (ou vazia).
	events, err := a.Store.Events()
	if err != nil {
		return err
	}
	if len(events) > 0 {
		switch bound := readRemote(a); {
		case bound == "":
			return fmt.Errorf("esta réplica tem %d eventos e não se sabe de que bucket são — rode `pnn sync` com a configuração antiga antes, ou `pnn logout`", len(events))
		case bound != cfg.Remote():
			return fmt.Errorf("esta réplica pertence a %s, e o login é em %s — `pnn logout` antes", bound, cfg.Remote())
		}
	} else if id := machineDeviceID(a.Dir); id != "" {
		if err := a.Store.InitDevice(id); err != nil {
			return err
		}
	}

	d.Since = a.Now().Format("02/01/2006 15:04")
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.Dir, 0o755); err != nil {
		return err
	}
	if err := writePrivate(filepath.Join(a.Dir, loginFile), raw); err != nil {
		return err
	}
	if err := runSync(ctx, a, bucket, cfg); err != nil {
		return fmt.Errorf("conectado, mas o primeiro sync falhou (%w) — rode `pnn sync`", err)
	}
	fmt.Fprintf(a.Out, "🔓 conectado a %s — continue de onde parou: %s\n", cfg.Remote(), th.Blue("pnn"))
	if cfg.BookPassphrase == "" {
		fmt.Fprintln(a.Out, th.Dim("sem a senha dos livros, os livros não descem — `pnn logout` e entre de novo com ela"))
	}
	return nil
}

// --- logout ---

type logoutOptions struct {
	yes, discard, discardBooks bool
}

func newLogoutCmd(a *app.App) *cobra.Command {
	var opts logoutOptions
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "sai deste dispositivo: sincroniza, confere que tudo está no bucket e apaga a réplica, os livros e a credencial",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return logoutWith(cmd.Context(), a, sync.NewBucket, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.yes, "sim", false, "não pergunta antes de apagar (a conferência continua)")
	cmd.Flags().BoolVar(&opts.discard, "descartar", false, "apaga sem sincronizar — perde o que não subiu (pede \"apagar\")")
	cmd.Flags().BoolVar(&opts.discardBooks, "descartar-livros", false, "apaga mesmo com livros que só existem neste dispositivo")
	return cmd
}

func logoutWith(ctx context.Context, a *app.App, open bucketOpener, opts logoutOptions) error {
	th := ui.Theme{On: a.Color}
	p := newPrompter(a)

	if opts.discard {
		fmt.Fprintln(a.Out, th.Warn("⚠ --descartar apaga sem sincronizar: o que este dispositivo não subiu se perde."))
		fmt.Fprint(a.Out, "Digite apagar para confirmar: ")
		if v, err := p.readLine(); err != nil || v != "apagar" {
			return errors.New("nada foi apagado")
		}
		return finishLogout(a, th)
	}

	cfg, _, err := syncConfig(a)
	if err != nil {
		return fmt.Errorf("sem configuração de sync não há como conferir que tudo está no bucket — nada foi apagado (`pnn login`, ou `pnn logout --descartar` para perder o que não subiu): %w", err)
	}
	bucket, err := open(ctx, cfg)
	if err != nil {
		return fmt.Errorf("%w — nada foi apagado", err)
	}
	if err := runSync(ctx, a, bucket, cfg); err != nil {
		return fmt.Errorf("o sync falhou (%w) — nada foi apagado", err)
	}
	mine, err := sync.VerifyOwn(ctx, a.Store, bucket, cfg.Prefix)
	if err != nil {
		return fmt.Errorf("a conferência falhou: %w — nada foi apagado", err)
	}

	// Livro guardado aqui sem cópia no bucket se perderia.
	objects, err := bucket.List(ctx, cfg.Prefix+"livros/")
	if err != nil {
		return fmt.Errorf("listar os livros do bucket: %w — nada foi apagado", err)
	}
	remote := map[string]bool{}
	for _, o := range objects {
		remote[o.Key] = true
	}
	files, _ := filepath.Glob(filepath.Join(booksDir(a), "*.epub"))
	state, err := loadBooks(a)
	if err != nil {
		return err
	}
	var onlyHere []string
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".epub")
		if !remote[sync.BookKey(cfg.Prefix, id)] {
			title := state.Books[id].Title
			if title == "" {
				title = id
			}
			onlyHere = append(onlyHere, title)
		}
	}
	if len(onlyHere) > 0 && !opts.discardBooks {
		return fmt.Errorf("estes livros só existem neste dispositivo: %s — entre com a senha dos livros (`pnn login`) e rode de novo, ou `--descartar-livros` — nada foi apagado", strings.Join(onlyHere, "; "))
	}

	fmt.Fprintf(a.Out, "✔ conferido: os %d eventos deste dispositivo e %d de %d livros estão no bucket\n",
		mine, len(files)-len(onlyHere), len(files))
	if !opts.yes && !p.confirm("Apagar a réplica, os livros e a credencial deste dispositivo?") {
		return errors.New("nada foi apagado")
	}
	return finishLogout(a, th)
}

// replicaFiles: o que é do pnn na pasta da réplica — e só isso se apaga
// (se PNN_HOME apontar para uma pasta com outras coisas, elas ficam).
var replicaFiles = []string{"events.jsonl", "device", "cursor", "lock", "last-view.json", loginFile, remoteFile}

func finishLogout(a *app.App, th ui.Theme) error {
	for _, name := range replicaFiles {
		if err := os.Remove(filepath.Join(a.Dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	temps, _ := filepath.Glob(filepath.Join(a.Dir, "events-*.jsonl"))
	tmpLogin, _ := filepath.Glob(filepath.Join(a.Dir, loginFile+".*.tmp"))
	for _, f := range append(temps, tmpLogin...) {
		os.Remove(f)
	}
	if err := os.RemoveAll(booksDir(a)); err != nil {
		return err
	}
	// As cópias de página que o editor deixa quando falha.
	pages, _ := filepath.Glob(filepath.Join(os.TempDir(), "pnn-*.md"))
	for _, f := range pages {
		os.Remove(f)
	}
	entries, _ := os.ReadDir(a.Dir)
	if len(entries) == 0 {
		os.Remove(a.Dir)
	}

	fmt.Fprintf(a.Out, "🔒 dispositivo limpo: réplica, livros e credencial apagados (%s)\n", a.Dir)
	if len(entries) > 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		fmt.Fprintln(a.Out, th.Warn("⚠ ficaram em "+a.Dir+" arquivos que não são do pnn: "+strings.Join(names, ", ")))
	}
	logoutWarnings(a, th)
	return nil
}

// Os rastros que o pnn não alcança.
var credentialVars = []string{"PNN_S3_BUCKET", "PNN_S3_ENDPOINT", "PNN_LIVROS_SENHA", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"}

func logoutWarnings(a *app.App, th ui.Theme) {
	var set []string
	for _, name := range credentialVars {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			set = append(set, name)
		}
	}
	if len(set) > 0 {
		fmt.Fprintln(a.Out, th.Warn("⚠ ainda exportadas neste shell: "+strings.Join(set, ", ")+" — o pnn não as apaga: tire do ~/.bashrc deste aparelho e feche o terminal"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, ".aws", "credentials")); err == nil {
			fmt.Fprintln(a.Out, th.Warn("⚠ ~/.aws/credentials existe — se tiver a chave do seu bucket, apague-a deste aparelho"))
		}
	}
	fmt.Fprintln(a.Out, th.Dim("o histórico do shell guarda os comandos (os títulos do `pnn t`): `history -c`; no aparelho de trabalho, HISTCONTROL=ignorespace e comandos começando com espaço"))
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		fmt.Fprintln(a.Out, th.Dim("no WSL, a central de notificações do Windows guarda os avisos do pnn (com títulos de tarefa): limpe-a; PNN_SILENCIO=1 evita"))
	}
}

// loginStatus: a linha do `pnn config`.
func loginStatus(a *app.App) string {
	login, err := loadLogin(a)
	switch {
	case err != nil:
		return "⚠ " + err.Error()
	case login == nil:
		return "sem login — o sync usa as variáveis abaixo"
	}
	return fmt.Sprintf("🔓 conectado a %s desde %s (login.json) — as variáveis de sincronização e credenciais abaixo não valem; `pnn logout` sai", login.config().Remote(), login.Since)
}
