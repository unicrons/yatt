// Package remote keeps the state database in S3 instead of on local disk.
//
// It is a transport around the SQLite store, not a second Store implementation:
// a command acquires a lock object, downloads the database to a private
// temporary file, runs against plain local SQLite, uploads the file back and
// releases the lock. SQLite's own file locking means nothing across hosts, so
// the lock object — acquired atomically with a conditional PutObject — is the
// only thing standing between two machines and a lost update.
package remote

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Client is the slice of the S3 API this package uses. aws-sdk-go-v2's
// *s3.Client satisfies it verbatim; tests substitute an in-memory fake that
// enforces the same conditional-write semantics the real service does.
type Client interface {
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	HeadObject(ctx context.Context, in *s3.HeadObjectInput, opts ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

const scheme = "s3://"

// IsRemote reports whether a --db value names an S3 object rather than a local
// file.
func IsRemote(path string) bool { return strings.HasPrefix(path, scheme) }

// parseURL splits s3://bucket/key/parts into bucket and key.
func parseURL(raw string) (bucket, key string, err error) {
	rest, ok := strings.CutPrefix(raw, scheme)
	if !ok {
		return "", "", fmt.Errorf("not an s3:// URL: %q", raw)
	}
	bucket, key, ok = strings.Cut(rest, "/")
	if !ok || bucket == "" || key == "" {
		return "", "", fmt.Errorf("invalid S3 database URL %q: want s3://bucket/key", raw)
	}
	return bucket, key, nil
}

// NewDefaultClient builds an S3 client from the standard AWS configuration
// chain (environment, shared config, SSO, IMDS). Custom endpoints via
// AWS_ENDPOINT_URL_S3 / AWS_ENDPOINT_URL are honored by the SDK itself; the
// only thing added here is path-style addressing when one is set, because
// self-hosted endpoints (MinIO and friends) usually cannot serve the
// virtual-hosted style.
func NewDefaultClient(ctx context.Context) (Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading AWS configuration: %w (are credentials and AWS_REGION available?)", err)
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		if os.Getenv("AWS_ENDPOINT_URL_S3") != "" || os.Getenv("AWS_ENDPOINT_URL") != "" {
			o.UsePathStyle = true
		}
	}), nil
}
