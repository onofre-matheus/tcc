// Implementação de Bucket sobre S3. Fica isolada aqui para o resto do sync
// (que é onde mora a lógica) rodar em teste sem rede.
package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/logging"
)

// Config vem do ambiente. As credenciais seguem a cadeia padrão da AWS
// (AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY, ~/.aws/credentials, AWS_PROFILE),
// então nada de segredo passa pelo ~/.pnn nem por flag na linha de comando.
type Config struct {
	Bucket   string // PNN_S3_BUCKET (obrigatório)
	Prefix   string // PNN_S3_PREFIX (padrão "pnn/")
	Endpoint string // PNN_S3_ENDPOINT (opcional: R2, MinIO, Backblaze…)
	Region   string // PNN_S3_REGION (padrão AWS_REGION, ou us-east-1)
}

var ErrNotConfigured = errors.New(
	"sync não configurado — defina PNN_S3_BUCKET (e as credenciais AWS)")

func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Bucket:   os.Getenv("PNN_S3_BUCKET"),
		Prefix:   os.Getenv("PNN_S3_PREFIX"),
		Endpoint: os.Getenv("PNN_S3_ENDPOINT"),
		Region:   os.Getenv("PNN_S3_REGION"),
	}
	if cfg.Bucket == "" {
		return Config{}, ErrNotConfigured
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "pnn/"
	}
	if !strings.HasSuffix(cfg.Prefix, "/") {
		cfg.Prefix += "/"
	}
	if cfg.Region == "" {
		if cfg.Region = os.Getenv("AWS_REGION"); cfg.Region == "" {
			cfg.Region = "us-east-1"
		}
	}
	return cfg, nil
}

type s3Bucket struct {
	client *s3.Client
	bucket string
}

// NewBucket monta o cliente. Com endpoint próprio, o endereçamento vai por
// caminho (`host/bucket/chave`): serviços compatíveis raramente têm o DNS por
// subdomínio que a AWS usa.
func NewBucket(ctx context.Context, cfg Config) (Bucket, error) {
	// O logger do SDK vai para o lixo: ele avisa coisas como "resposta sem
	// checksum" (comum em serviços compatíveis) direto no terminal, no meio da
	// saída do comando. Erro de verdade sobe pelo retorno, não pelo log.
	aws0, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithLogger(logging.Nop{}),
	)
	if err != nil {
		return nil, fmt.Errorf("credenciais AWS: %w", err)
	}
	client := s3.NewFromConfig(aws0, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		}
	})
	return &s3Bucket{client: client, bucket: cfg.Bucket}, nil
}

func (b *s3Bucket) List(ctx context.Context, prefix string) ([]Object, error) {
	var objects []Object
	pages := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.bucket),
		Prefix: aws.String(prefix),
	})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Contents {
			objects = append(objects, Object{
				Key:  aws.ToString(item.Key),
				ETag: aws.ToString(item.ETag),
			})
		}
	}
	return objects, nil
}

func (b *s3Bucket) Get(ctx context.Context, key string) ([]byte, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (b *s3Bucket) Put(ctx context.Context, key string, body []byte) (string, error) {
	out, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(b.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("application/x-ndjson"),
	})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.ETag), nil
}
