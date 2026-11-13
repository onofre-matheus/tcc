// A fase de livros do sync (spec/SPEC.md §5, "Livros no bucket"): depois dos
// eventos, cada livro do log que existe só aqui sobe cifrado, e cada um que
// existe só no bucket desce, é decifrado, conferido pelo sha256 do cadastro e
// guardado na pasta de livros do pnn. O usuário cadastra uma vez; onde o
// arquivo mora é problema do pnn.
//
// Como os objetos .jsonl, o objeto do livro é imutável e tem dono único (o
// book_id é aleatório): subir de novo o mesmo livro só regrava os mesmos
// bytes cifrados de outro jeito, e baixar é sempre seguro. Nada aqui lê o
// payload de evento nenhum — quem diz quais livros existem é a projeção, do
// lado de quem chama.
package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// BookRef é o que a fase de livros precisa saber de cada livro do log.
type BookRef struct {
	ID, Title, SHA256 string
	Archived          bool
}

type BookTransfer struct {
	Title string
	Bytes int
}

// BooksResult é o que o `pnn sync` imprime da fase de livros.
type BooksResult struct {
	Sent, Received []BookTransfer
	Failed         []string // "Título: motivo"
	// Waiting: sem a senha dos livros, quantos teriam subido ou descido.
	Waiting int
}

// bookIterations é o custo do PBKDF2 ao selar; os testes o baixam.
var bookIterations = BookIterations

// LocalBookPath é onde o pnn guarda o EPUB do livro neste dispositivo.
func LocalBookPath(dir, bookID string) string { return filepath.Join(dir, bookID+".epub") }

// SyncBooks sobe o que é só local e baixa o que é só remoto. Sem senha, só
// conta (Waiting). Um livro com problema não para os outros: vai para Failed.
func SyncBooks(ctx context.Context, bucket Bucket, prefix, passphrase string, books []BookRef, dir string) (BooksResult, error) {
	objects, err := bucket.List(ctx, prefix+booksDir)
	if err != nil {
		return BooksResult{}, fmt.Errorf("listar os livros do bucket: %w", err)
	}
	remote := map[string]bool{}
	for _, obj := range objects {
		name := strings.TrimPrefix(obj.Key, prefix+booksDir)
		if id, ok := strings.CutSuffix(name, BookSuffix); ok && id != "" && !strings.Contains(id, "/") {
			remote[id] = true
		}
	}

	var res BooksResult
	fail := func(b BookRef, err error) { res.Failed = append(res.Failed, b.Title+": "+err.Error()) }
	for _, b := range books {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		path := LocalBookPath(dir, b.ID)
		_, statErr := os.Stat(path)
		local := statErr == nil
		switch {
		case local && !remote[b.ID]:
			if passphrase == "" {
				res.Waiting++
				continue
			}
			n, err := uploadBook(ctx, bucket, prefix, passphrase, b, path)
			if err != nil {
				fail(b, err)
				continue
			}
			res.Sent = append(res.Sent, BookTransfer{Title: b.Title, Bytes: n})

		case !local && remote[b.ID] && !b.Archived:
			if passphrase == "" {
				res.Waiting++
				continue
			}
			n, err := downloadBook(ctx, bucket, prefix, passphrase, b, dir)
			if err != nil {
				fail(b, err)
				continue
			}
			res.Received = append(res.Received, BookTransfer{Title: b.Title, Bytes: n})
		}
	}
	return res, nil
}

func uploadBook(ctx context.Context, bucket Bucket, prefix, passphrase string, b BookRef, path string) (int, error) {
	epub, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if sum := sha256Hex(epub); sum != b.SHA256 {
		return 0, errors.New("a cópia local não confere com o cadastro (sha256) — não subiu")
	}
	blob, err := SealBook(passphrase, b.ID, epub, bookIterations)
	if err != nil {
		return 0, err
	}
	if _, err := bucket.Put(ctx, BookKey(prefix, b.ID), blob); err != nil {
		return 0, err
	}
	return len(epub), nil
}

func downloadBook(ctx context.Context, bucket Bucket, prefix, passphrase string, b BookRef, dir string) (int, error) {
	blob, err := bucket.Get(ctx, BookKey(prefix, b.ID))
	if err != nil {
		return 0, err
	}
	epub, err := OpenBook(passphrase, b.ID, blob)
	if err != nil {
		return 0, err
	}
	if sum := sha256Hex(epub); sum != b.SHA256 {
		return 0, errors.New("o arquivo do bucket não confere com o cadastro (sha256) — não foi guardado")
	}
	return len(epub), WriteBookFile(dir, b.ID, epub)
}

// WriteBookFile guarda o EPUB na pasta de livros: temporário + rename, para
// uma queda no meio não deixar livro pela metade; pasta 0700, arquivo 0600.
func WriteBookFile(dir, bookID string, epub []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, bookID+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(epub); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, fs.ErrPermission) {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), LocalBookPath(dir, bookID))
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
