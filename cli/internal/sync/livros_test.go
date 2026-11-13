package sync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// O vetor de conformidade cruzada: o mesmo arquivo é lido pelo teste da
// extensão (extension/test/livro_cifra.test.js) — Go e Web Crypto produzem
// o mesmo blob, byte a byte, e abrem o do outro.
func TestLivroCifradoVetorCruzado(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "spec", "sync", "livro_cifrado.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Passphrase string `json:"passphrase"`
		BookID     string `json:"book_id"`
		Iterations int    `json:"iterations"`
		Salt       string `json:"salt_hex"`
		Nonce      string `json:"nonce_hex"`
		EPUB       string `json:"epub_base64"`
		Blob       string `json:"blob_base64"`
		MustFail   []struct {
			Why        string  `json:"why"`
			Passphrase *string `json:"passphrase"`
			BookID     *string `json:"book_id"`
			FlipByte   *int    `json:"flip_byte"`
		} `json:"must_fail"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	salt, _ := hex.DecodeString(v.Salt)
	nonce, _ := hex.DecodeString(v.Nonce)
	epub, _ := base64.StdEncoding.DecodeString(v.EPUB)
	want, _ := base64.StdEncoding.DecodeString(v.Blob)

	blob, err := sealWith(v.Passphrase, v.BookID, epub, v.Iterations, salt, nonce)
	if err != nil || !bytes.Equal(blob, want) {
		t.Fatalf("selar com sal e nonce fixos dá o blob do vetor:\n%x\n%x (%v)", blob, want, err)
	}
	got, err := OpenBook(v.Passphrase, v.BookID, want)
	if err != nil || !bytes.Equal(got, epub) {
		t.Fatalf("abrir devolve o EPUB: %q %v", got, err)
	}
	for _, f := range v.MustFail {
		pass, id, b := v.Passphrase, v.BookID, bytes.Clone(want)
		if f.Passphrase != nil {
			pass = *f.Passphrase
		}
		if f.BookID != nil {
			id = *f.BookID
		}
		if f.FlipByte != nil {
			b[*f.FlipByte] ^= 0xFF
		}
		if _, err := OpenBook(pass, id, b); !errors.Is(err, ErrBookAuthFail) {
			t.Errorf("%s: esperava falha de autenticação, veio %v", f.Why, err)
		}
	}
}

func TestLivroCifradoCabecalho(t *testing.T) {
	blob, err := SealBook("s", "b1", []byte("epub"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(blob, []byte("PNNLIVRO\x01")) || binary.BigEndian.Uint32(blob[9:]) != BookIterations {
		t.Fatalf("cabeçalho com a versão e 600 000 iterações: %x", blob[:13])
	}
	other, _ := SealBook("s", "b1", []byte("epub"), 0)
	if bytes.Equal(blob, other) {
		t.Fatal("sal e nonce aleatórios: o mesmo livro cifra diferente a cada vez")
	}
	if bytes.Contains(blob, []byte("epub")) {
		t.Fatal("o conteúdo não pode aparecer no objeto")
	}
	if _, err := OpenBook("s", "b1", []byte("PK\x03\x04 um epub qualquer, não cifrado.....................")); !errors.Is(err, ErrNotBookBlob) {
		t.Fatalf("o que não é .pnnlivro é recusado antes de derivar chave: %v", err)
	}
	if _, err := SealBook("", "b1", []byte("x"), 1); err == nil {
		t.Fatal("senha vazia não cifra")
	}
}

func shaOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Dois dispositivos: o A tem o arquivo e sobe; o B, com outra pasta, baixa,
// confere e guarda. Sem senha, só conta; senha errada e cópia corrompida
// falham sem parar os outros; livro arquivado não desce.
func TestSyncBooksEntreDispositivos(t *testing.T) {
	bookIterations = 1000
	t.Cleanup(func() { bookIterations = BookIterations })
	ctx := context.Background()
	bucket := newFakeBucket()
	epub1, epub2 := []byte("conteúdo do livro um"), []byte("livro dois, arquivado")
	books := []BookRef{
		{ID: "b1", Title: "Livro um", SHA256: shaOf(epub1)},
		{ID: "b2", Title: "Livro dois", SHA256: shaOf(epub2), Archived: true},
	}
	dirA, dirB := t.TempDir(), t.TempDir()
	if err := WriteBookFile(dirA, "b1", epub1); err != nil {
		t.Fatal(err)
	}
	if err := WriteBookFile(dirA, "b2", epub2); err != nil {
		t.Fatal(err)
	}

	res, err := SyncBooks(ctx, bucket, "pnn/", "", books, dirA)
	if err != nil || res.Waiting != 2 || len(bucket.objects) != 0 {
		t.Fatalf("sem senha, nada sobe — só conta: %+v %v", res, err)
	}

	res, err = SyncBooks(ctx, bucket, "pnn/", "senha", books, dirA)
	if err != nil || len(res.Sent) != 2 || res.Sent[0].Title != "Livro um" || res.Sent[0].Bytes != len(epub1) {
		t.Fatalf("A sobe os dois: %+v %v", res, err)
	}
	for key, body := range bucket.objects {
		if !strings.HasPrefix(key, "pnn/livros/") || !strings.HasSuffix(key, ".pnnlivro") || bytes.Contains(body, epub1) {
			t.Fatalf("no bucket só objeto cifrado com id: %s", key)
		}
	}
	res, _ = SyncBooks(ctx, bucket, "pnn/", "senha", books, dirA)
	if len(res.Sent)+len(res.Received) != 0 {
		t.Fatalf("de novo, nada a fazer: %+v", res)
	}

	res, err = SyncBooks(ctx, bucket, "pnn/", "outra senha", books, dirB)
	if err != nil || len(res.Received) != 0 || len(res.Failed) != 1 || !strings.Contains(res.Failed[0], "Livro um: não abriu") {
		t.Fatalf("senha errada falha o livro, não o sync: %+v %v", res, err)
	}
	res, err = SyncBooks(ctx, bucket, "pnn/", "senha", books, dirB)
	if err != nil || len(res.Received) != 1 || res.Received[0].Title != "Livro um" {
		t.Fatalf("B baixa só o não arquivado: %+v %v", res, err)
	}
	got, err := os.ReadFile(LocalBookPath(dirB, "b1"))
	if err != nil || !bytes.Equal(got, epub1) {
		t.Fatalf("o EPUB guardado é o original: %q %v", got, err)
	}
	if info, _ := os.Stat(LocalBookPath(dirB, "b1")); info.Mode().Perm() != 0o600 {
		t.Fatalf("arquivo 0600: %v", info.Mode())
	}

	// cadastro que não confere com o arquivo (cópia corrompida): não sobe
	os.WriteFile(LocalBookPath(dirB, "b3"), []byte("corrompido"), 0o600)
	bad := append(books, BookRef{ID: "b3", Title: "Livro três", SHA256: shaOf([]byte("o original"))})
	res, _ = SyncBooks(ctx, bucket, "pnn/", "senha", bad, dirB)
	if len(res.Sent) != 0 || len(res.Failed) != 1 || !strings.Contains(res.Failed[0], "não confere") {
		t.Fatalf("cópia que não confere não sobe: %+v", res)
	}
}
