package remote

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/andoniaf/yatt/internal/store"
)

// Store is the remote-backed store: a plain SQLite store over a downloaded
// copy of the database, plus a Close that uploads the copy back and releases
// the lock. Embedding promotes every data method unchanged — the remote layer
// has no opinion about the data, only about where the file lives.
type Store struct {
	*store.SQLite

	client Client
	rawURL string
	bucket string
	key    string
	tmpDir string
	// etag identifies the exact object version this run started from; empty
	// means the database did not exist yet. The final upload is conditional on
	// it, so a concurrent write that somehow bypassed the lock (a force-unlock)
	// turns into a loud error instead of a silent overwrite.
	etag string
	// preHash is the downloaded file's content hash. Close compares against it
	// so read-only commands never pay an upload, and a rolled-back write is
	// indistinguishable from no write at all.
	preHash [sha256.Size]byte
	// ctx is the command context captured at Open. Close derives from it with
	// the cancellation stripped: an upload and a lock release must be attempted
	// even when the run that made them necessary is being torn down.
	ctx context.Context
}

var _ store.Store = (*Store)(nil)

// Open acquires the remote lock, downloads the database (a missing object is
// first use and starts a fresh one) and opens it as a local SQLite store.
// op names the running command; it is recorded in the lock so the holder of a
// stale lock can be identified.
func Open(ctx context.Context, client Client, rawURL, op string) (*Store, error) {
	bucket, key, err := parseURL(rawURL)
	if err != nil {
		return nil, err
	}

	if err := acquireLock(ctx, client, bucket, key, rawURL, op); err != nil {
		return nil, err
	}
	// From here every failure must give the lock back, or an error path would
	// manufacture the stale lock the error is reporting.
	fail := func(err error) (*Store, error) {
		cleanupCtx := context.WithoutCancel(ctx)
		if unlockErr := releaseLock(cleanupCtx, client, bucket, key); unlockErr != nil {
			err = errors.Join(err, unlockErr)
		}
		return nil, err
	}

	tmpDir, err := os.MkdirTemp("", "yatt-remote-*")
	if err != nil {
		return fail(fmt.Errorf("creating temporary directory: %w", err))
	}
	failCleanup := func(err error) (*Store, error) {
		_ = os.RemoveAll(tmpDir)
		return fail(err)
	}

	dbPath := filepath.Join(tmpDir, "yatt.db")
	etag, err := download(ctx, client, bucket, key, dbPath)
	if err != nil {
		return failCleanup(err)
	}

	preHash, err := hashFile(dbPath)
	if err != nil {
		return failCleanup(err)
	}

	sqlite, err := store.Open(dbPath)
	if err != nil {
		return failCleanup(err)
	}

	return &Store{
		SQLite:  sqlite,
		client:  client,
		rawURL:  rawURL,
		bucket:  bucket,
		key:     key,
		tmpDir:  tmpDir,
		etag:    etag,
		preHash: preHash,
		ctx:     ctx,
	}, nil
}

// Close closes the SQLite store, uploads the database if this run changed it,
// and releases the lock. The SQLite close must come first: it checkpoints the
// WAL into the main file, and uploading before that would ship a file missing
// its latest transactions.
func (s *Store) Close() error {
	var errs []error
	if err := s.SQLite.Close(); err != nil {
		errs = append(errs, err)
	}

	// The command context may already be cancelled or expired by now; cleanup
	// still has to run, so only its values are kept.
	ctx := context.WithoutCancel(s.ctx)
	dbPath := filepath.Join(s.tmpDir, "yatt.db")

	uploadFailed := false
	if changed, err := s.changed(dbPath); err != nil {
		uploadFailed = true
		errs = append(errs, err)
	} else if changed {
		if err := s.upload(ctx, dbPath); err != nil {
			uploadFailed = true
			errs = append(errs, err)
		}
	}

	// The lock is released even after a failed upload: the remote database is
	// unchanged and valid, so holding the lock would block everyone for a
	// failure only this machine can act on.
	if err := releaseLock(ctx, s.client, s.bucket, s.key); err != nil {
		errs = append(errs, err)
	}

	if uploadFailed {
		errs = append(errs, fmt.Errorf("the database with this run's changes survives at %s; recover with: yatt state push %s --db %s --force", dbPath, dbPath, s.rawURL))
	} else {
		_ = os.RemoveAll(s.tmpDir)
	}
	return errors.Join(errs...)
}

// changed reports whether the database file differs from what was downloaded.
func (s *Store) changed(dbPath string) (bool, error) {
	hash, err := hashFile(dbPath)
	if err != nil {
		return false, err
	}
	return hash != s.preHash, nil
}

// upload writes the database back, conditionally on the object still being the
// version this run downloaded.
func (s *Store) upload(ctx context.Context, dbPath string) error {
	file, err := os.Open(dbPath)
	if err != nil {
		return fmt.Errorf("reading database for upload: %w", err)
	}
	defer func() { _ = file.Close() }()

	in := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(s.key),
		Body:        file,
		ContentType: aws.String("application/vnd.sqlite3"),
	}
	if s.etag != "" {
		in.IfMatch = aws.String(s.etag)
	} else {
		// First upload ever: refuse to clobber a database somebody else created
		// in the meantime, which can only happen across a force-unlock.
		in.IfNoneMatch = aws.String("*")
	}
	if _, err := s.client.PutObject(ctx, in); err != nil {
		if isConditionFailed(err) {
			return fmt.Errorf("uploading database s3://%s/%s: the remote changed while this command held the lock (was the lock force-cleared?): %w", s.bucket, s.key, err)
		}
		return fmt.Errorf("uploading database s3://%s/%s: %w", s.bucket, s.key, err)
	}
	return nil
}

// hashFile is the change detector. A missing file hashes as empty, which is
// exactly right: first use starts from no database, and "still no file" means
// nothing to upload.
func hashFile(path string) ([sha256.Size]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return [sha256.Size]byte{}, fmt.Errorf("hashing %s: %w", path, err)
	}
	return sha256.Sum256(data), nil
}
