package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// download fetches the database object into path. A missing object is not an
// error but first use: found is false and no file is written. found is
// reported separately from the ETag because an endpoint may serve the object
// without one — "no object" and "no ETag to guard the upload with" must not
// be conflated, or a run against such an endpoint would try to create an
// object that already exists.
func download(ctx context.Context, c Client, bucket, key, path string) (etag string, found bool, err error) {
	out, err := c.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("downloading database s3://%s/%s: %w", bucket, key, err)
	}
	defer func() { _ = out.Body.Close() }()

	if err := writeFile(path, out.Body); err != nil {
		return "", false, fmt.Errorf("writing database copy %s: %w", path, err)
	}
	return aws.ToString(out.ETag), true, nil
}

// writeFile streams r to path and syncs it, so the file the store opens is
// complete on disk rather than sitting in page cache.
func writeFile(path string, r io.Reader) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ReadLock reports who currently holds the lock, without touching it.
// ErrNoLock when nobody does.
func ReadLock(ctx context.Context, c Client, rawURL string) (LockInfo, error) {
	bucket, key, err := parseURL(rawURL)
	if err != nil {
		return LockInfo{}, err
	}
	return readLock(ctx, c, bucket, key)
}

// Unlock force-clears the lock the caller examined. It exists for exactly one
// situation — a process died holding the lock — and the caller is expected to
// have confirmed that with the user, because clearing a live holder's lock
// reopens the door to concurrent writers.
//
// expect is the lock the caller read and showed the user. The lock is re-read
// and compared against it before deleting: while the user deliberated, the
// stale-looking holder may have finished and a live one taken its place, and
// deleting whatever happens to be there now would clear a lock nobody ever
// looked at. The re-read shrinks that window from deliberation-length to the
// gap between two requests; S3 offers no conditional delete to close it
// entirely.
func Unlock(ctx context.Context, c Client, rawURL string, expect LockInfo) error {
	bucket, key, err := parseURL(rawURL)
	if err != nil {
		return err
	}
	current, err := readLock(ctx, c, bucket, key)
	if err != nil {
		return err
	}
	if current != expect {
		return fmt.Errorf("the lock changed hands while you decided: it is now %s — run yatt state unlock again to inspect the new holder", current.Describe())
	}
	return releaseLock(ctx, c, bucket, key)
}

// Push uploads a local database file as the remote one — the migration path
// from a local-only setup, and the recovery path after a failed upload. It
// takes the lock like any writer. Without force it refuses to replace an
// existing remote database, and the refusal is enforced by the conditional
// write itself rather than by a look-then-write race.
func Push(ctx context.Context, c Client, localPath, rawURL string, force bool) error {
	bucket, key, err := parseURL(rawURL)
	if err != nil {
		return err
	}

	// A non-empty WAL sidecar means some process has the database open (or died
	// without checkpointing); uploading just the main file would ship a
	// database missing its latest transactions.
	if info, err := os.Stat(localPath + "-wal"); err == nil && info.Size() > 0 {
		return fmt.Errorf("%s has a non-empty WAL sidecar: close any yatt process using it before pushing", localPath)
	}

	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("opening local database: %w", err)
	}
	defer func() { _ = file.Close() }()

	if err := acquireLock(ctx, c, bucket, key, rawURL, "state push"); err != nil {
		return err
	}
	defer func() { _ = releaseLock(context.WithoutCancel(ctx), c, bucket, key) }()

	in := &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        file,
		ContentType: aws.String("application/vnd.sqlite3"),
	}
	if !force {
		in.IfNoneMatch = aws.String("*")
	}
	if _, err := c.PutObject(ctx, in); err != nil {
		if isConditionFailed(err) {
			return fmt.Errorf("a remote database already exists at %s: pass --force to replace it", rawURL)
		}
		return fmt.Errorf("uploading database %s: %w", rawURL, err)
	}
	return nil
}

// Pull downloads the remote database to localPath. It takes no lock: a single
// GET is atomic in S3, so the copy is a consistent snapshot even if a writer
// is active — merely up to that writer's upload out of date.
func Pull(ctx context.Context, c Client, rawURL, localPath string, force bool) error {
	bucket, key, err := parseURL(rawURL)
	if err != nil {
		return err
	}
	if !force {
		if _, err := os.Stat(localPath); err == nil {
			return fmt.Errorf("%s already exists: pass --force to overwrite it", localPath)
		}
	}

	out, err := c.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return fmt.Errorf("no remote database exists at %s", rawURL)
		}
		return fmt.Errorf("downloading database %s: %w", rawURL, err)
	}
	defer func() { _ = out.Body.Close() }()

	// The body streams into a sibling temp file that is renamed into place
	// only once it is complete, so an interrupted download can neither
	// truncate the copy already at localPath nor leave a half-written file
	// that passes for a finished one.
	tmp, err := os.CreateTemp(filepath.Dir(localPath), ".yatt-pull-*")
	if err != nil {
		return fmt.Errorf("creating temporary file for %s: %w", localPath, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := io.Copy(tmp, out.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("downloading database %s: %w", rawURL, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", localPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", localPath, err)
	}
	if err := os.Rename(tmp.Name(), localPath); err != nil {
		return fmt.Errorf("writing %s: %w", localPath, err)
	}
	return nil
}
