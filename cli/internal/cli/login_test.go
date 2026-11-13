package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/sync"
)

// O login e o logout do aparelho de trabalho: entra, restaura, trabalha,
// sai — e só se apaga o que está comprovadamente no bucket.

// sessionEnv: o shell de quem roda o teste não pode valer (o do Matheus tem
// o bucket real configurado).
func sessionEnv(t *testing.T) {
	t.Helper()
	for _, name := range append(credentialVars, "PNN_S3_PREFIX", "PNN_S3_REGION", "AWS_PROFILE", "AWS_REGION") {
		t.Setenv(name, "")
	}
	t.Setenv("TMPDIR", t.TempDir())
	id := filepath.Join(t.TempDir(), "machine-id")
	os.WriteFile(id, []byte("maquina-do-trabalho\n"), 0o644)
	old := machineIDPath
	machineIDPath = id
	t.Cleanup(func() { machineIDPath = old })
}

func opener(b sync.Bucket) bucketOpener {
	return func(context.Context, sync.Config) (sync.Bucket, error) { return b, nil }
}

var conta = sync.Config{Bucket: "meu-bucket", Prefix: "pnn/", BookPassphrase: "s3nha"}

// O que se digita no login: bucket, endpoint, região, prefixo, access key,
// secret, senha dos livros.
const respostas = "meu-bucket\n\n\npnn/\nAKIAEXEMPLO\nsegredo-da-aws\ns3nha\n"

func out(a *app.App) string { return a.Out.(*bytes.Buffer).String() }

// O aparelho pessoal já tem tudo no bucket (uma tarefa e um livro).
func pessoal(t *testing.T, bucket *memBucket) {
	t.Helper()
	p := internalApp(t)
	epub := filepath.Join(t.TempDir(), "livro.epub")
	writeTestEPUB(t, epub, "Livro do pessoal")
	cmdRun(t, p, "livro", epub)
	cmdRun(t, p, "t", "Tarefa criada em casa")
	if err := runSync(context.Background(), p, bucket, conta); err != nil {
		t.Fatal(err)
	}
}

func TestLoginRestauraELogoutApagaEVolta(t *testing.T) {
	sessionEnv(t)
	bucket := &memBucket{objects: map[string][]byte{}}
	pessoal(t, bucket)

	w := internalApp(t) // o aparelho de trabalho, vazio
	w.In = strings.NewReader(respostas)
	if err := loginWith(context.Background(), w, opener(bucket)); err != nil {
		t.Fatalf("%v\n%s", err, out(w))
	}
	if !strings.Contains(out(w), "conectado a s3://aws/meu-bucket/pnn/") || !strings.Contains(out(w), "▼ 📖 Livro do pessoal") {
		t.Fatalf("login restaura eventos e livros:\n%s", out(w))
	}
	info, err := os.Stat(filepath.Join(w.Dir, loginFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("login.json 0600: %v %v", info, err)
	}
	device, _ := w.Store.DeviceID()
	if device != machineDeviceID(w.Dir) {
		t.Fatalf("o dispositivo vem da máquina: %s", device)
	}
	if dia := cmdRun(t, w, "dia"); !strings.Contains(dia, "Tarefa criada em casa") {
		t.Fatalf("continua de onde parou:\n%s", dia)
	}
	w.In = strings.NewReader("")
	w.Out.(*bytes.Buffer).Reset()
	loginWith(context.Background(), w, opener(bucket))
	if !strings.Contains(out(w), "já conectado") {
		t.Fatalf("login de novo só diz que já está: %s", out(w))
	}

	// Trabalha: uma tarefa nova, uma página que o editor deixou para trás,
	// e um arquivo que não é do pnn na pasta.
	cmdRun(t, w, "t", "Tarefa criada no trabalho")
	os.WriteFile(filepath.Join(os.TempDir(), "pnn-rascunho-123.md"), []byte("texto da página"), 0o600)
	os.WriteFile(filepath.Join(w.Dir, "meu-arquivo.txt"), []byte("x"), 0o644)

	w.Out.(*bytes.Buffer).Reset()
	if err := logoutWith(context.Background(), w, opener(bucket), logoutOptions{yes: true}); err != nil {
		t.Fatalf("%v\n%s", err, out(w))
	}
	if !strings.Contains(out(w), "✔ conferido: os 1 eventos deste dispositivo e 1 de 1 livros estão no bucket") ||
		!strings.Contains(out(w), "🔒 dispositivo limpo") || !strings.Contains(out(w), "meu-arquivo.txt") {
		t.Fatalf("logout confere e limpa:\n%s", out(w))
	}
	for _, name := range append(replicaFiles, "livros") {
		if _, err := os.Stat(filepath.Join(w.Dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s ficou no dispositivo", name)
		}
	}
	if pages, _ := filepath.Glob(filepath.Join(os.TempDir(), "pnn-*.md")); len(pages) != 0 {
		t.Fatalf("a página temporária ficou: %v", pages)
	}
	if _, err := os.Stat(filepath.Join(w.Dir, "meu-arquivo.txt")); err != nil {
		t.Fatal("o que não é do pnn não se apaga")
	}

	// Amanhã: login de novo na mesma máquina — o mesmo dispositivo, nada perdido.
	w.In = strings.NewReader(respostas)
	w.Out.(*bytes.Buffer).Reset()
	if err := loginWith(context.Background(), w, opener(bucket)); err != nil {
		t.Fatalf("%v\n%s", err, out(w))
	}
	if again, _ := w.Store.DeviceID(); again != device {
		t.Fatalf("o mesmo objeto no bucket: %s ≠ %s", again, device)
	}
	dia := cmdRun(t, w, "dia")
	if !strings.Contains(dia, "Tarefa criada no trabalho") || !strings.Contains(dia, "Tarefa criada em casa") {
		t.Fatalf("tudo de volta:\n%s", dia)
	}
	objects := 0
	for key := range bucket.objects {
		if strings.HasSuffix(key, ".jsonl.zst") {
			objects++
		}
	}
	if objects != 2 {
		t.Fatalf("um objeto por aparelho, não um por login: %d", objects)
	}
}

type failingBucket struct{ *memBucket }

func (failingBucket) Put(context.Context, string, []byte) (string, error) {
	return "", errors.New("S3 PUT 503: indisponível")
}

func TestLogoutNaoApagaSemConferir(t *testing.T) {
	sessionEnv(t)
	bucket := &memBucket{objects: map[string][]byte{}}
	pessoal(t, bucket)
	w := internalApp(t)
	w.In = strings.NewReader(respostas)
	if err := loginWith(context.Background(), w, opener(bucket)); err != nil {
		t.Fatal(err)
	}
	cmdRun(t, w, "t", "Ainda não subiu")
	before, _ := w.Store.Events()

	// o sync falha: nada se apaga
	err := logoutWith(context.Background(), w, opener(failingBucket{bucket}), logoutOptions{yes: true})
	if err == nil || !strings.Contains(err.Error(), "nada foi apagado") {
		t.Fatalf("sync falho: %v", err)
	}
	if after, _ := w.Store.Events(); len(after) != len(before) {
		t.Fatal("os eventos continuam")
	}

	// dizer não à confirmação: nada se apaga
	w.In = strings.NewReader("n\n")
	if err := logoutWith(context.Background(), w, opener(bucket), logoutOptions{}); err == nil {
		t.Fatal("sem confirmação, não apaga")
	}
	if _, err := os.Stat(filepath.Join(w.Dir, loginFile)); err != nil {
		t.Fatal("a sessão continua")
	}

	// livro só local (o bucket perdeu o objeto, e não há senha para subir)
	for key := range bucket.objects {
		if strings.Contains(key, "/livros/") {
			delete(bucket.objects, key)
		}
	}
	login, _ := loadLogin(w)
	login.BookPassphrase = ""
	raw, _ := json.Marshal(login)
	writePrivate(filepath.Join(w.Dir, loginFile), raw)
	err = logoutWith(context.Background(), w, opener(bucket), logoutOptions{yes: true})
	if err == nil || !strings.Contains(err.Error(), "só existem neste dispositivo: Livro do pessoal") {
		t.Fatalf("livro só local: %v", err)
	}
	if err := logoutWith(context.Background(), w, opener(bucket), logoutOptions{yes: true, discardBooks: true}); err != nil {
		t.Fatalf("--descartar-livros segue: %v", err)
	}
}

func TestLogoutSemConfiguracaoSoComDescartar(t *testing.T) {
	sessionEnv(t)
	a := internalApp(t)
	cmdRun(t, a, "t", "Tarefa local")
	if err := logoutWith(context.Background(), a, opener(nil), logoutOptions{yes: true}); err == nil || !strings.Contains(err.Error(), "nada foi apagado") {
		t.Fatalf("sem como conferir, não apaga: %v", err)
	}
	a.In = strings.NewReader("sim\n")
	if err := logoutWith(context.Background(), a, opener(nil), logoutOptions{discard: true}); err == nil {
		t.Fatal("--descartar pede a palavra apagar")
	}
	a.In = strings.NewReader("apagar\n")
	if err := logoutWith(context.Background(), a, opener(nil), logoutOptions{discard: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("--descartar apaga a réplica")
	}
}

// A trava réplica ↔ bucket: a réplica que sincronizou com um bucket não fala
// com outro — nem pelo sync, nem pelo login.
func TestReplicaPresaAoBucket(t *testing.T) {
	sessionEnv(t)
	a := internalApp(t)
	cmdRun(t, a, "t", "Tarefa do bucket A")
	bucketA, bucketB := &memBucket{objects: map[string][]byte{}}, &memBucket{objects: map[string][]byte{}}
	if err := runSync(context.Background(), a, bucketA, sync.Config{Bucket: "a", Prefix: "pnn/"}); err != nil {
		t.Fatal(err)
	}
	err := runSync(context.Background(), a, bucketB, sync.Config{Bucket: "b", Prefix: "pnn/"})
	if err == nil || !strings.Contains(err.Error(), "pertence a s3://aws/a/pnn/") || len(bucketB.objects) != 0 {
		t.Fatalf("o sync recusa o outro bucket, sem subir nada: %v %d", err, len(bucketB.objects))
	}
	a.In = strings.NewReader("b\n\n\npnn/\nAKIA\nsegredo\n\n")
	if err := loginWith(context.Background(), a, opener(bucketB)); err == nil || !strings.Contains(err.Error(), "pertence a s3://aws/a/pnn/") {
		t.Fatalf("o login também: %v", err)
	}

	sem := internalApp(t)
	cmdRun(t, sem, "t", "Nunca sincronizou")
	sem.In = strings.NewReader("b\n\n\npnn/\nAKIA\nsegredo\n\n")
	if err := loginWith(context.Background(), sem, opener(bucketB)); err == nil || !strings.Contains(err.Error(), "não se sabe de que bucket") {
		t.Fatalf("réplica com dados de origem desconhecida: %v", err)
	}
}

// Com login, vale a credencial dele — não a do shell.
func TestLoginVenceOAmbiente(t *testing.T) {
	sessionEnv(t)
	bucket := &memBucket{objects: map[string][]byte{}}
	a := internalApp(t)
	a.In = strings.NewReader(respostas)
	if err := loginWith(context.Background(), a, opener(bucket)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PNN_S3_BUCKET", "bucket-do-shell")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "segredo-do-shell")
	cfg, fromLogin, err := syncConfig(a)
	if err != nil || !fromLogin || cfg.Bucket != "meu-bucket" || cfg.SecretAccessKey != "segredo-da-aws" {
		t.Fatalf("o login vale: %+v %v %v", cfg, fromLogin, err)
	}
	if status := cmdRun(t, a, "config"); !strings.Contains(strings.Join(strings.Fields(status), " "), "conectado a s3://aws/meu-bucket/pnn/") || strings.Contains(status, "segredo-da-aws") {
		t.Fatalf("o config diz que há login e não mostra segredo:\n%s", status)
	}
}

// O bucket diz que gravou, mas o objeto não tem tudo (upload truncado, proxy,
// consistência): a conferência relê e o logout não apaga.
type truncatingBucket struct{ *memBucket }

func (b truncatingBucket) Put(ctx context.Context, key string, body []byte) (string, error) {
	if strings.HasSuffix(key, ".jsonl.zst") {
		body = body[:len(body)/2]
	}
	return b.memBucket.Put(ctx, key, body)
}

func TestLogoutConfereOQueOBucketGuardou(t *testing.T) {
	sessionEnv(t)
	bucket := &memBucket{objects: map[string][]byte{}}
	w := internalApp(t)
	w.In = strings.NewReader(respostas)
	if err := loginWith(context.Background(), w, opener(bucket)); err != nil {
		t.Fatal(err)
	}
	for i := range 40 {
		cmdRun(t, w, "t", "tarefa do trabalho "+strings.Repeat("x", i))
	}
	err := logoutWith(context.Background(), w, opener(truncatingBucket{bucket}), logoutOptions{yes: true})
	if err == nil || !strings.Contains(err.Error(), "a conferência falhou") || !strings.Contains(err.Error(), "nada foi apagado") {
		t.Fatalf("objeto incompleto no bucket: %v", err)
	}
	if events, _ := w.Store.Events(); len(events) != 40 {
		t.Fatalf("a réplica fica inteira: %d", len(events))
	}
}
