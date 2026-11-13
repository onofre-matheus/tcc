// `pnn sync` — troca eventos com o bucket S3 (spec/SPEC.md §5). Uma operação
// só, idempotente: pode rodar quantas vezes quiser, de quantas máquinas
// quiser, em qualquer ordem.
//
// Depois dos eventos vem a fase de livros (SPEC §5, "Livros no bucket"): os
// EPUBs guardados só aqui sobem cifrados com a senha dos livros
// (PNN_LIVROS_SENHA), e os que estão só no bucket descem, decifrados e
// conferidos pelo sha256 do cadastro. Sem a senha, os eventos sincronizam e
// os livros esperam.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/sync"
	"github.com/onofre-matheus/tcc/cli/internal/ui"
	"github.com/spf13/cobra"
)

// syncTimeout: o sync é uma operação de rede num comando interativo — melhor
// falhar rápido e pedir de novo do que pendurar o terminal.
const syncTimeout = 60 * time.Second

// booksTimeout: livro é arquivo de dezenas de MB; a fase de livros tem o seu.
const booksTimeout = 10 * time.Minute

func newSyncCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "troca eventos com o bucket (envia os seus, recebe os dos outros) e leva e traz os livros cifrados",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := syncConfig(a) // o do login, se houver; senão o ambiente
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), syncTimeout)
			defer cancel()

			bucket, err := sync.NewBucket(ctx, cfg)
			if err != nil {
				return err
			}
			return runSync(cmd.Context(), a, bucket, cfg)
		},
	}
}

// runSync faz as duas fases e imprime o resultado; o bucket vem de fora
// (nos testes, um bucket em memória).
func runSync(parent context.Context, a *app.App, bucket sync.Bucket, cfg sync.Config) error {
	// A trava réplica ↔ bucket (login.go): a réplica que já sincronizou com
	// um bucket não fala com outro.
	if err := checkRemote(a, cfg); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, syncTimeout)
	defer cancel()
	result, err := sync.Run(ctx, a.Store, bucket, cfg.Prefix)
	if err != nil {
		return err
	}
	if err := bindRemote(a, cfg); err != nil {
		return err
	}
	th := ui.Theme{On: a.Color}
	fmt.Fprintf(a.Out, "▲ %d enviado(s) · ▼ %d recebido(s)\n", result.Sent, result.Received)

	books, err := syncBooks(parent, a, bucket, cfg)
	if err != nil {
		// os eventos já foram: o livro fica para o próximo sync
		fmt.Fprintln(a.Out, th.Warn("⚠ livros: "+err.Error()))
	}
	for _, b := range books.Sent {
		fmt.Fprintf(a.Out, "▲ 📖 %s %s\n", b.Title, th.Dim("("+humanBytes(b.Bytes)+", cifrado)"))
	}
	for _, b := range books.Received {
		fmt.Fprintf(a.Out, "▼ 📖 %s %s\n", b.Title, th.Dim("("+humanBytes(b.Bytes)+")"))
	}
	for _, f := range books.Failed {
		fmt.Fprintln(a.Out, th.Warn("⚠ 📖 "+f))
	}
	if books.Waiting > 0 {
		fmt.Fprintf(a.Out, "📖 %s %s\n", plural(books.Waiting, "livro espera", "livros esperam"),
			th.Dim("PNN_LIVROS_SENHA para ir e vir pelo bucket (cifrados)"))
	}
	if result.Received > 0 || len(books.Received) > 0 {
		fmt.Fprintf(a.Out, "→ %s\n", th.Blue("pnn"))
	}
	return nil
}

// syncBooks entrega à fase de livros os livros do log (os ativos primeiro).
func syncBooks(parent context.Context, a *app.App, bucket sync.Bucket, cfg sync.Config) (sync.BooksResult, error) {
	state, err := loadBooks(a)
	if err != nil {
		return sync.BooksResult{}, err
	}
	var refs []sync.BookRef
	for _, id := range sortedBookIDs(state) {
		b := state.Books[id]
		refs = append(refs, sync.BookRef{ID: id, Title: b.Title, SHA256: b.SHA256, Archived: b.Archived})
	}
	if len(refs) == 0 {
		return sync.BooksResult{}, nil
	}
	ctx, cancel := context.WithTimeout(parent, booksTimeout)
	defer cancel()
	return sync.SyncBooks(ctx, bucket, cfg.Prefix, cfg.BookPassphrase, refs, booksDir(a))
}

// humanBytes: "830 kB", "12,3 MB".
func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return strings.Replace(fmt.Sprintf("%.1f MB", float64(n)/(1<<20)), ".", ",", 1)
	case n >= 1<<10:
		return fmt.Sprintf("%d kB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}
