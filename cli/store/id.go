// Geração de identificadores UUIDv7 (spec/SPEC.md §1).
// Prefixo de timestamp de 48 bits + versão/variante + aleatório — os ids
// crescem monotonicamente no tempo, o que ajuda ordenação e depuração.
package store

import (
	"crypto/rand"
	"fmt"
	"time"
)

func UUIDv7() string {
	return uuidv7At(time.Now())
}

func uuidv7At(t time.Time) string {
	var b [16]byte

	ms := uint64(t.UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}

	if _, err := rand.Read(b[6:]); err != nil {
		// crypto/rand só falha se o sistema estiver sem fonte de entropia;
		// não há degradação razoável para um gerador de id único.
		panic(fmt.Sprintf("sem entropia para gerar uuid: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x70 // versão 7
	b[8] = (b[8] & 0x3f) | 0x80 // variante RFC 4122

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
