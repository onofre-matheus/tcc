// O EPUB no bucket, cifrado (spec/SPEC.md §5, "Livros no bucket"). O arquivo
// do livro não é evento: é transporte, como os objetos .jsonl — mas, ao
// contrário deles, sobe cifrado, para que o bucket guarde só bytes opacos e
// nenhum livro vaze por ele.
//
// Formato .pnnlivro (o mesmo em extension/storage/livro_cifra.js):
//
//	"PNNLIVRO" | versão 0x01 | iterações (uint32 BE) | sal (16) | nonce (12) |
//	AES-256-GCM(EPUB)  (texto cifrado + tag de 16)
//
// A chave sai da senha dos livros por PBKDF2-HMAC-SHA256 (sal por objeto); o
// AAD é o cabeçalho inteiro mais o book_id — senha errada, cabeçalho mexido
// ou objeto trocado de livro falham na autenticação. Formato próprio não
// esconde nada sozinho (o repositório é público): quem protege é a senha.
package sync

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	bookMagic     = "PNNLIVRO"
	bookVersion   = 1
	saltSize      = 16
	nonceSize     = 12
	bookHeaderLen = len(bookMagic) + 1 + 4 + saltSize + nonceSize

	// BookIterations é o custo do PBKDF2 de quem grava (OWASP, 2023).
	BookIterations = 600_000
	// maxIterations limita o que se aceita ao ler: o cabeçalho é
	// autenticado, mas a derivação acontece antes da autenticação.
	maxIterations = 10_000_000

	// BookSuffix é o sufixo do objeto no bucket: <prefixo>livros/<id>.pnnlivro.
	BookSuffix = ".pnnlivro"
	booksDir   = "livros/"
)

var (
	ErrNotBookBlob  = errors.New("não é um livro do pnn (.pnnlivro)")
	ErrBookAuthFail = errors.New("não abriu: a senha dos livros não confere (ou o arquivo foi alterado)")
)

// BookKey é a chave do objeto do livro no bucket.
func BookKey(prefix, bookID string) string { return prefix + booksDir + bookID + BookSuffix }

// SealBook cifra o EPUB do livro com a senha. iterations ≤ 0 = BookIterations.
func SealBook(passphrase, bookID string, epub []byte, iterations int) ([]byte, error) {
	salt := make([]byte, saltSize)
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return sealWith(passphrase, bookID, epub, iterations, salt, nonce)
}

// sealWith é o SealBook com sal e nonce dados — o vetor de conformidade
// cruzada (spec/sync/livro_cifrado.json) precisa deles fixos.
func sealWith(passphrase, bookID string, epub []byte, iterations int, salt, nonce []byte) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("senha dos livros vazia")
	}
	if iterations <= 0 {
		iterations = BookIterations
	}
	header := make([]byte, 0, bookHeaderLen)
	header = append(header, bookMagic...)
	header = append(header, bookVersion)
	header = binary.BigEndian.AppendUint32(header, uint32(iterations))
	header = append(header, salt...)
	header = append(header, nonce...)

	aead, err := bookAEAD(passphrase, salt, iterations)
	if err != nil {
		return nil, err
	}
	return aead.Seal(header, nonce, epub, bookAAD(header, bookID)), nil
}

// OpenBook decifra o objeto do livro; erro de autenticação = senha errada,
// objeto alterado ou de outro livro.
func OpenBook(passphrase, bookID string, blob []byte) ([]byte, error) {
	if len(blob) < bookHeaderLen+16 || !bytes.HasPrefix(blob, []byte(bookMagic)) {
		return nil, ErrNotBookBlob
	}
	if v := blob[len(bookMagic)]; v != bookVersion {
		return nil, fmt.Errorf("%w: versão %d desconhecida", ErrNotBookBlob, v)
	}
	at := len(bookMagic) + 1
	iterations := int(binary.BigEndian.Uint32(blob[at:]))
	if iterations < 1 || iterations > maxIterations {
		return nil, fmt.Errorf("%w: %d iterações", ErrNotBookBlob, iterations)
	}
	salt := blob[at+4 : at+4+saltSize]
	nonce := blob[at+4+saltSize : bookHeaderLen]
	aead, err := bookAEAD(passphrase, salt, iterations)
	if err != nil {
		return nil, err
	}
	header := blob[:bookHeaderLen]
	epub, err := aead.Open(nil, nonce, blob[bookHeaderLen:], bookAAD(header, bookID))
	if err != nil {
		return nil, ErrBookAuthFail
	}
	return epub, nil
}

func bookAEAD(passphrase string, salt []byte, iterations int) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, passphrase, salt, iterations, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func bookAAD(header []byte, bookID string) []byte {
	aad := make([]byte, 0, len(header)+len(bookID))
	return append(append(aad, header...), bookID...)
}
