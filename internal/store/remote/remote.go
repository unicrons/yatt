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

	"github.com/unicrons/yatt/internal/store"
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
	// existed records whether the remote object was there at Open; when it was
	// not, the first upload refuses to clobber one that appeared since.
	existed bool
	// etag identifies the exact object version this run started from. The
	// final upload is conditional on it, so a concurrent write that somehow
	// bypassed the lock (a force-unlock) turns into a loud error instead of a
	// silent overwrite. It can be empty even for an existing object — not
	// every endpoint serves an ETag — in which case the upload has no guard
	// to offer.
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
	etag, existed, err := download(ctx, client, bucket, key, dbPath)
	if err != nil {
		return failCleanup(err)
	}

	if !existed {
		// First use: materialize the schema (open runs the migrations, close
		// checkpoints them) before taking the baseline hash, so the change
		// gate compares this run's work against an empty-but-migrated
		// database. Hashing nothing instead would make the migrations
		// themselves read as a change, and a read-only command against a
		// missing — or mistyped — key would manufacture and upload a junk
		// database object.
		empty, err := store.Open(dbPath)
		if err != nil {
			return failCleanup(err)
		}
		if err := empty.Close(); err != nil {
			return failCleanup(err)
		}
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
		existed: existed,
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
	sqliteErr := s.SQLite.Close()
	if sqliteErr != nil {
		errs = append(errs, sqliteErr)
	}

	// The command context may already be cancelled or expired by now; cleanup
	// still has to run, so only its values are kept.
	ctx := context.WithoutCancel(s.ctx)
	dbPath := filepath.Join(s.tmpDir, "yatt.db")

	preserve := false
	switch {
	case sqliteErr != nil:
		// An unclean SQLite close means the WAL may hold transactions that
		// were never checkpointed into the main file. Uploading would ship a
		// database missing this run's writes, and deleting the directory
		// would destroy their only copy — so do neither, and say where the
		// files are.
		preserve = true
		errs = append(errs, fmt.Errorf("upload skipped: the database did not close cleanly, so its files (including any write-ahead log) are preserved at %s", s.tmpDir))
	default:
		changed, err := s.changed(dbPath)
		if err != nil {
			preserve = true
			errs = append(errs, err, s.recoveryHint(dbPath))
		} else if changed {
			if err := s.upload(ctx, dbPath); err != nil {
				preserve = true
				errs = append(errs, err, s.recoveryHint(dbPath))
			}
		}
	}

	// The lock is released even after a failed upload: the remote database is
	// unchanged and valid, so holding the lock would block everyone for a
	// failure only this machine can act on.
	if err := releaseLock(ctx, s.client, s.bucket, s.key); err != nil {
		errs = append(errs, err)
	}

	if !preserve {
		_ = os.RemoveAll(s.tmpDir)
	}
	return errors.Join(errs...)
}

// recoveryHint names the surviving copy and the command that re-uploads it.
func (s *Store) recoveryHint(dbPath string) error {
	return fmt.Errorf("the database with this run's changes survives at %s; recover with: yatt state push %s --db %s --force", dbPath, dbPath, s.rawURL)
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
	switch {
	case !s.existed:
		// First upload ever: refuse to clobber a database somebody else created
		// in the meantime, which can only happen across a force-unlock.
		in.IfNoneMatch = aws.String("*")
	case s.etag != "":
		in.IfMatch = aws.String(s.etag)
	default:
		// The endpoint served the object without an ETag: there is nothing to
		// condition the write on, so it goes unguarded rather than failing
		// every run against such an endpoint.
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
