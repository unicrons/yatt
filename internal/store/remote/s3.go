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
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Client is the slice of the S3 API this package uses. aws-sdk-go-v2's
// *s3.Client satisfies it verbatim; tests substitute an in-memory fake that
// enforces the same conditional-write semantics the real service does.
type Client interface {
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

const scheme = "s3://"

// IsRemote reports whether a --db value names an S3 object rather than a local
// file. The scheme comparison is case-insensitive (RFC 3986 schemes are), and
// it has to be: "S3://bucket/key" mistaken for a local path would not error —
// it would create a fresh local database and silently fork the history the
// remote one exists to share.
func IsRemote(path string) bool {
	return len(path) >= len(scheme) && strings.EqualFold(path[:len(scheme)], scheme)
}

// parseURL splits s3://bucket/key/parts into bucket and key.
func parseURL(raw string) (bucket, key string, err error) {
	if !IsRemote(raw) {
		return "", "", fmt.Errorf("not an s3:// URL: %q", raw)
	}
	bucket, key, ok := strings.Cut(raw[len(scheme):], "/")
	if !ok || bucket == "" || key == "" {
		return "", "", fmt.Errorf("invalid S3 database URL %q: want s3://bucket/key", raw)
	}
	return bucket, key, nil
}

// NewDefaultClient builds an S3 client from the standard AWS configuration
// chain (environment, shared config, SSO, IMDS) for the bucket named in
// rawURL. Custom endpoints via AWS_ENDPOINT_URL_S3 / AWS_ENDPOINT_URL are
// honored by the SDK itself; the only thing added here is path-style
// addressing when one is set, because self-hosted endpoints (MinIO and
// friends) usually cannot serve the virtual-hosted style.
//
// The bucket's region is discovered from the bucket itself rather than
// trusted from local config: a bucket lives in exactly one region, and the
// user already named the bucket — requiring them to also know and export its
// region would only reproduce S3's PermanentRedirect error whenever the two
// disagree. Discovery reads the x-amz-bucket-region header off a HeadBucket
// probe, which S3 serves even cross-region and on permission-denied
// responses.
func NewDefaultClient(ctx context.Context, rawURL string) (Client, error) {
	bucket, _, err := parseURL(rawURL)
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading AWS configuration: %w (are credentials available?)", err)
	}

	customEndpoint := os.Getenv("AWS_ENDPOINT_URL_S3") != "" || os.Getenv("AWS_ENDPOINT_URL") != ""
	if cfg.Region == "" {
		// The discovery probe must be signed against some region; any works,
		// since the response names the right one either way.
		cfg.Region = "us-east-1"
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if customEndpoint {
			o.UsePathStyle = true
		}
	})

	// A custom endpoint is one host serving every bucket: there is no region
	// to discover and no redirect to avoid.
	if customEndpoint {
		return client, nil
	}

	region, err := manager.GetBucketRegion(ctx, client, bucket)
	if err != nil || region == "" {
		// Best effort: a failed probe (no credentials, no such bucket, no
		// network) is the first operation's error to report with full
		// context, not the probe's.
		return client, nil
	}
	if region != cfg.Region {
		cfg.Region = region
		client = s3.NewFromConfig(cfg)
	}
	return client, nil
}
