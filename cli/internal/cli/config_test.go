package cli

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// A tabela do `pnn config` é a fonte da verdade: toda variável que o código
// lê tem de estar nela, e nada nela pode estar morto. As AWS_* são lidas pelo
// SDK da AWS, fora deste código — as únicas dispensadas da segunda regra.
func TestConfigListaTodaVariavelQueOCodigoLe(t *testing.T) {
	read := map[string]bool{}
	direct := regexp.MustCompile(`(?:Getenv|LookupEnv|orDefault)\("([A-Z0-9_]+)"`)
	first := regexp.MustCompile(`firstEnv\(([^)]*)\)`)
	literal := regexp.MustCompile(`"([A-Z0-9_]+)"`)
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range direct.FindAllStringSubmatch(string(src), -1) {
			read[m[1]] = true
		}
		for _, m := range first.FindAllStringSubmatch(string(src), -1) {
			for _, name := range literal.FindAllStringSubmatch(m[1], -1) {
				read[name[1]] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, v := range envVars {
		listed = append(listed, v.name)
	}
	for name := range read {
		if !slices.Contains(listed, name) {
			t.Errorf("o pnn lê %s, mas o `pnn config` não a lista — acrescente em envVars (config.go)", name)
		}
	}
	for _, name := range listed {
		if !read[name] && !strings.HasPrefix(name, "AWS_") {
			t.Errorf("%s está no `pnn config`, mas nenhum código a lê", name)
		}
	}
	for _, v := range envVars {
		if !slices.Contains(configGroups, v.group) || v.purpose == "" {
			t.Errorf("%s sem grupo conhecido ou sem propósito", v.name)
		}
	}
}

// Segredo nunca aparece — nem na tela, nem no --json; o resto mostra o valor
// e, vazio, o que vale de fato.
func TestConfigNuncaMostraSegredo(t *testing.T) {
	for _, v := range envVars { // o ambiente de quem roda o teste não vaza
		t.Setenv(v.name, "")
	}
	t.Setenv("PNN_LIVROS_SENHA", "senha-que-nao-pode-aparecer")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-segredo-que-nao-pode-aparecer")
	t.Setenv("PNN_S3_BUCKET", "meu-bucket")
	t.Setenv("PNN_PAGER", "cat")
	a := internalApp(t)

	out := cmdRun(t, a, "config")
	if strings.Contains(out, "nao-pode-aparecer") {
		t.Fatalf("segredo na tela:\n%s", out)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := len([]rune(line)); w > 80 {
			t.Errorf("linha %d com %d colunas (fora do terminal, 80): %q", i, w, line)
		}
	}
	flat := strings.Join(strings.Fields(out), " ") // as descrições quebram linha
	for _, want := range []string{
		"Sincronização (pnn sync)",
		" PNN_S3_BUCKET         meu-bucket",
		" PNN_LIVROS_SENHA      definida (segredo — o valor não aparece)",
		"na extensão: bookPassphrase",
		" PNN_S3_PREFIX         —  → pnn/",
		" PNN_HOME              —  → " + a.Dir,
		" PNN_TZ                —  → America/Sao_Paulo",
		" AWS_ACCESS_KEY_ID     —",
		" PNN_PAGER             cat",
	} {
		if !strings.Contains(flat, strings.Join(strings.Fields(want), " ")) {
			t.Fatalf("falta %q em:\n%s", want, out)
		}
	}
	if alias := cmdRun(t, a, "configuracao"); alias != out {
		t.Fatal("configuracao é o mesmo comando")
	}

	raw := cmdRun(t, a, "config", "--json")
	if strings.Contains(raw, "nao-pode-aparecer") {
		t.Fatalf("segredo no --json:\n%s", raw)
	}
	var entries []configEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatal(err)
	}
	byName := map[string]configEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if e := byName["PNN_LIVROS_SENHA"]; !e.Set || !e.Secret || e.Value != nil {
		t.Fatalf("segredo: definido, sem valor: %+v", e)
	}
	if e := byName["PNN_S3_BUCKET"]; e.Value == nil || *e.Value != "meu-bucket" {
		t.Fatalf("o resto mostra o valor: %+v", e)
	}
	if len(entries) != len(envVars) {
		t.Fatalf("todas as variáveis: %d de %d", len(entries), len(envVars))
	}
}
