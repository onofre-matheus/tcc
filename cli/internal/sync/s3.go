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

// defaultRegion é o último recurso: nem PNN_S3_REGION nem a cadeia da AWS
// disseram nada. Serviços compatíveis costumam aceitar qualquer região.
const defaultRegion = "us-east-1"

var (
	ErrNotConfigured = errors.New(
		"sync não configurado — defina PNN_S3_BUCKET (e as credenciais AWS)")

	ErrNoCredentials = errors.New(
		"credenciais AWS não encontradas — defina AWS_ACCESS_KEY_ID e " +
			"AWS_SECRET_ACCESS_KEY, ou AWS_PROFILE apontando para ~/.aws/credentials")
)

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
	// Região fica vazia de propósito quando não há PNN_S3_REGION: quem resolve
	// é a cadeia da AWS (AWS_REGION, AWS_DEFAULT_REGION, região do perfil em
	// ~/.aws/config). Fixar um padrão aqui atropelaria o perfil e assinaria na
	// região errada — falha que só aparece como SignatureDoesNotMatch.
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
	opts := []func(*config.LoadOptions) error{config.WithLogger(logging.Nop{})}
	if cfg.Region != "" {
		opts = append(opts, config.WithRegion(cfg.Region))
	}
	aws0, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("configuração AWS: %w", err)
	}
	if aws0.Region == "" {
		aws0.Region = defaultRegion // nem PNN_S3_REGION nem a cadeia disseram
	}

	// Checagem adiantada: sem isso, credencial ausente vira um parágrafo sobre
	// EC2 IMDS no meio de "listar o bucket". Com chave provisionada por fora,
	// esta é a falha mais provável — merece uma frase que diga o que fazer.
	if _, err := aws0.Credentials.Retrieve(ctx); err != nil {
		return nil, ErrNoCredentials
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
