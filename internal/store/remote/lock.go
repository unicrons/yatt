package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// Version is the yatt version recorded in new lock objects. The cmd package
// sets it at startup to whatever `yatt --version` reports; it stays "devel" for
// builds with no version wiring (and in tests).
var Version = "devel"

// LockInfo is the body of the lock object: enough for the holder of a stale
// lock to be identified from another machine, where a pid alone would say
// nothing.
type LockInfo struct {
	Hostname  string    `json:"hostname"`
	PID       int       `json:"pid"`
	CreatedAt time.Time `json:"created_at"`
	Version   string    `json:"version"`
	Operation string    `json:"operation"`
}

// Describe renders the holder for humans. The lock-held abort and `yatt state
// unlock` both show it, so the two descriptions can never drift apart.
func (i LockInfo) Describe() string {
	return fmt.Sprintf("held by %s (pid %d) since %s, yatt %s, operation %q",
		i.Hostname, i.PID, i.CreatedAt.UTC().Format(time.RFC3339),
		i.Version, i.Operation)
}

// ErrNoLock reports that no lock object exists where one was asked about.
var ErrNoLock = errors.New("no lock is held")

// ErrLockHeld is returned when the lock object already exists. It names the
// holder so the reader can decide whether the lock is live (wait and retry by
// hand) or stale (a crashed process, cleared with `yatt state unlock`).
type ErrLockHeld struct {
	Info LockInfo
	URL  string
	// ReadErr is set when the lock exists but its body could not be fetched or
	// decoded, in which case Info is zero.
	ReadErr error
}

func (e *ErrLockHeld) Error() string {
	if e.ReadErr != nil {
		return fmt.Sprintf("remote state is locked and the holder metadata is unreadable (%v) — if no other yatt is running, run: yatt state unlock --db %s", e.ReadErr, e.URL)
	}
	return fmt.Sprintf("remote state is locked: %s — if that process is gone, run: yatt state unlock --db %s",
		e.Info.Describe(), e.URL)
}

// lockKey derives the lock object's key from the database object's key. Being
// a sibling of the database keeps both under whatever prefix the user chose.
func lockKey(key string) string { return key + ".lock" }

// acquireLock takes the lock or fails, never waits. The conditional PutObject
// is the atomicity: If-None-Match makes S3 itself reject the write when the
// object exists, so two clients racing for the lock cannot both win no matter
// how their requests interleave.
func acquireLock(ctx context.Context, c Client, bucket, key, rawURL, op string) error {
	body, err := json.Marshal(LockInfo{
		Hostname:  hostname(),
		PID:       os.Getpid(),
		CreatedAt: time.Now().UTC(),
		Version:   Version,
		Operation: op,
	})
	if err != nil {
		return fmt.Errorf("encoding lock metadata: %w", err)
	}

	// One retry covers the benign race where the holder releases between our
	// failed conditional put and our read of its metadata.
	for attempt := 0; ; attempt++ {
		_, err := c.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(lockKey(key)),
			Body:        bytes.NewReader(body),
			ContentType: aws.String("application/json"),
			IfNoneMatch: aws.String("*"),
		})
		if err == nil {
			return nil
		}
		if !isConditionFailed(err) {
			return fmt.Errorf("acquiring lock s3://%s/%s: %w", bucket, lockKey(key), err)
		}

		info, readErr := readLock(ctx, c, bucket, key)
		if errors.Is(readErr, ErrNoLock) {
			if attempt == 0 {
				continue
			}
			// Twice in a row the conditional put lost to a lock that was gone
			// again by the time it was read: the lock is cycling between other
			// short commands. That is contention, not a stale lock — steering
			// the user to force-unlock here would have them clear a live one.
			return fmt.Errorf("the remote database is busy (its lock keeps changing hands): retry in a moment")
		}
		if readErr != nil {
			return &ErrLockHeld{URL: rawURL, ReadErr: readErr}
		}
		return &ErrLockHeld{Info: info, URL: rawURL}
	}
}

// readLock fetches and decodes the lock object; ErrNoLock when it is absent.
func readLock(ctx context.Context, c Client, bucket, key string) (LockInfo, error) {
	out, err := c.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(lockKey(key)),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return LockInfo{}, fmt.Errorf("s3://%s/%s: %w", bucket, lockKey(key), ErrNoLock)
		}
		return LockInfo{}, fmt.Errorf("reading lock s3://%s/%s: %w", bucket, lockKey(key), err)
	}
	defer func() { _ = out.Body.Close() }()

	raw, err := io.ReadAll(out.Body)
	if err != nil {
		return LockInfo{}, fmt.Errorf("reading lock s3://%s/%s: %w", bucket, lockKey(key), err)
	}
	var info LockInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return LockInfo{}, fmt.Errorf("decoding lock s3://%s/%s: %w", bucket, lockKey(key), err)
	}
	return info, nil
}

// releaseLock deletes the lock object. Deleting an already-absent key succeeds
// in S3, which is the semantics wanted here: release must be safe to attempt
// on every exit path.
func releaseLock(ctx context.Context, c Client, bucket, key string) error {
	_, err := c.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(lockKey(key)),
	})
	if err != nil {
		return fmt.Errorf("releasing lock s3://%s/%s: %w (clear it with: yatt state unlock)", bucket, lockKey(key), err)
	}
	return nil
}

// isConditionFailed recognizes the two ways S3 rejects a conditional write:
// 412 PreconditionFailed when the object exists, and 409
// ConditionalRequestConflict when another conditional write on the same key is
// in flight. Both mean "somebody else holds it".
func isConditionFailed(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "PreconditionFailed", "ConditionalRequestConflict":
		return true
	}
	return false
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown-host"
	}
	return name
}
