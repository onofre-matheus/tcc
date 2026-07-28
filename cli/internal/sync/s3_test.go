// O caminho do SDK só se prova falando S3 de verdade: um servidor mínimo
// responde ListObjectsV2/GET/PUT e o teste verifica que endpoint próprio,
// endereçamento por caminho e assinatura chegam como se espera.
package sync

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeS3(t *testing.T, objects map[string]string) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)

		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
			var contents strings.Builder
			for key, body := range objects {
				fmt.Fprintf(&contents,
					"<Contents><Key>%s</Key><ETag>&quot;%d&quot;</ETag><Size>%d</Size></Contents>",
					key, len(body), len(body))
			}
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
<Name>meu-bucket</Name><KeyCount>%d</KeyCount><IsTruncated>false</IsTruncated>%s
</ListBucketResult>`, len(objects), contents.String())

		case r.Method == http.MethodGet:
			key := strings.TrimPrefix(r.URL.Path, "/meu-bucket/")
			body, ok := objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write([]byte(body))

		case r.Method == http.MethodPut:
			key := strings.TrimPrefix(r.URL.Path, "/meu-bucket/")
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("ler corpo do PUT: %v", err)
			}
			objects[key] = string(body)
			w.Header().Set("ETag", fmt.Sprintf("%q", fmt.Sprint(len(body))))
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func s3Credentials(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "chave-de-teste")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "segredo-de-teste")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/inexistente")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/inexistente")
}

func TestBucketS3FalaComEndpointProprio(t *testing.T) {
	s3Credentials(t)
	objects := map[string]string{
		"pnn/dev-a.jsonl": `{"id":"e1","type":"task.created","v":1,"lc":1,` +
			`"ts":"2026-07-27T10:00:00.000Z","device":"dev-a","payload":{"task_id":"t1"}}` + "\n",
	}
	srv, seen := fakeS3(t, objects)

	bucket, err := NewBucket(context.Background(), Config{
		Bucket:   "meu-bucket",
		Prefix:   "pnn/",
		Endpoint: srv.URL,
		Region:   "us-east-1",
	})
	if err != nil {
		t.Fatalf("montar o cliente: %v", err)
	}
	ctx := context.Background()

	listed, err := bucket.List(ctx, "pnn/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 || listed[0].Key != "pnn/dev-a.jsonl" {
		t.Fatalf("List devolveu %+v", listed)
	}
	if listed[0].ETag == "" {
		t.Fatal("o ETag alimenta o cursor; não pode vir vazio")
	}

	body, err := bucket.Get(ctx, "pnn/dev-a.jsonl")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if events := parseLog(body); len(events) != 1 || events[0].ID != "e1" {
		t.Fatalf("Get trouxe %q", body)
	}

	etag, err := bucket.Put(ctx, "pnn/dev-b.jsonl", []byte("{\"id\":\"e2\"}\n"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if etag == "" {
		t.Fatal("Put deveria devolver o ETag novo para o cursor")
	}
	if objects["pnn/dev-b.jsonl"] == "" {
		t.Fatal("o objeto não chegou ao servidor")
	}

	// Endereçamento por caminho: a chave vem depois do bucket na URL, e não
	// como subdomínio (que serviços compatíveis costumam não ter).
	for _, req := range *seen {
		if !strings.Contains(req, "/meu-bucket") {
			t.Fatalf("esperava endereçamento por caminho, veio %q", req)
		}
	}
}

func TestConfigFromEnvExigeBucketENormalizaPrefixo(t *testing.T) {
	t.Setenv("PNN_S3_BUCKET", "")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("sem bucket, o sync tem de dizer o que falta em vez de tentar")
	}

	t.Setenv("PNN_S3_BUCKET", "meu-bucket")
	t.Setenv("PNN_S3_PREFIX", "estudos")
	t.Setenv("PNN_S3_REGION", "")
	t.Setenv("AWS_REGION", "")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Prefix != "estudos/" {
		t.Fatalf("o prefixo precisa da barra para não colar na chave: %q", cfg.Prefix)
	}
	if cfg.Region == "" {
		t.Fatal("região precisa de padrão — o SDK recusa vazio")
	}
}
