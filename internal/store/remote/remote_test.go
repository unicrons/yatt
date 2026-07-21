// These tests run the real flow — lock, download, real SQLite in a temp dir,
// upload — against the in-memory fake, which enforces S3's conditional-write
// semantics. What is faked is the network, not the behavior.
package remote_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/andoniaf/yatt/internal/store"
	"github.com/andoniaf/yatt/internal/store/remote"
	"github.com/andoniaf/yatt/internal/store/remote/remotetest"
)

const (
	dbURL   = "s3://bucket/state/yatt.db"
	dbKey   = "bucket/state/yatt.db"
	lockKey = "bucket/state/yatt.db.lock"
)

func finding(candidate string) store.Finding {
	return store.Finding{
		Candidate:   candidate,
		Registrable: candidate,
		Technique:   "omission",
		Registered:  true,
	}
}

func TestOpenRecordCloseRoundTrips(t *testing.T) {
	fake := remotetest.NewFake()
	ctx := context.Background()

	s, err := remote.Open(ctx, fake, dbURL, "scan")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.RecordScan(ctx, "example.com", "", []store.Finding{finding("xample.com")}); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, ok := fake.Objects[dbKey]; !ok {
		t.Fatalf("no database object uploaded at %s", dbKey)
	}
	if _, ok := fake.Objects[lockKey]; ok {
		t.Errorf("lock object still present after Close")
	}

	// A second client — nothing local survives — sees the recorded scan.
	s2, err := remote.Open(ctx, fake, dbURL, "history")
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer func() { _ = s2.Close() }()
	scan, findings, err := s2.LastScan(ctx, "example.com")
	if err != nil {
		t.Fatalf("LastScan: %v", err)
	}
	if scan == nil || len(findings) != 1 || findings[0].Candidate != "xample.com" {
		t.Errorf("LastScan = %v, %v; want the scan recorded by the first client", scan, findings)
	}
}

func TestReadOnlyCloseDoesNotUpload(t *testing.T) {
	fake := remotetest.NewFake()
	ctx := context.Background()

	s, err := remote.Open(ctx, fake, dbURL, "scan")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.RecordScan(ctx, "example.com", "", []store.Finding{finding("xample.com")}); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	uploads := fake.PutCount[dbKey]

	s2, err := remote.Open(ctx, fake, dbURL, "history")
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if _, err := s2.ListScans(ctx, "example.com"); err != nil {
		t.Fatalf("ListScans: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("read-only Close: %v", err)
	}

	if fake.PutCount[dbKey] != uploads {
		t.Errorf("read-only command uploaded the database: %d puts, want %d", fake.PutCount[dbKey], uploads)
	}
}

func TestUploadFailurePreservesTheDatabaseAndReleasesTheLock(t *testing.T) {
	fake := remotetest.NewFake()
	ctx := context.Background()

	s, err := remote.Open(ctx, fake, dbURL, "scan")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.RecordScan(ctx, "example.com", "", []store.Finding{finding("xample.com")}); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}

	fake.FailPut[dbKey] = errors.New("scripted upload failure")
	err = s.Close()
	if err == nil {
		t.Fatal("Close succeeded despite the upload failing")
	}
	if !strings.Contains(err.Error(), "state push") || !strings.Contains(err.Error(), "--force") {
		t.Errorf("Close error gives no recovery command: %v", err)
	}
	if _, ok := fake.Objects[lockKey]; ok {
		t.Errorf("lock object still present after failed upload")
	}

	// The surviving file is named in the error and must actually exist, or the
	// recovery command it recommends would fail.
	path := pathFromError(t, err.Error())
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("preserved database %s: %v", path, statErr)
	} else {
		_ = os.RemoveAll(strings.TrimSuffix(path, "/yatt.db"))
	}
}

// pathFromError digs the preserved temp path out of the recovery message.
func pathFromError(t *testing.T, msg string) string {
	t.Helper()
	for _, word := range strings.Fields(msg) {
		if strings.Contains(word, "yatt-remote-") && strings.HasSuffix(word, "yatt.db") {
			return word
		}
	}
	t.Fatalf("no preserved path in error: %s", msg)
	return ""
}

func TestConcurrentReplacementIsDetectedAtUpload(t *testing.T) {
	fake := remotetest.NewFake()
	ctx := context.Background()

	// Seed a remote database so Open records its ETag.
	s, err := remote.Open(ctx, fake, dbURL, "scan")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.RecordScan(ctx, "example.com", "", []store.Finding{finding("xample.com")}); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := remote.Open(ctx, fake, dbURL, "scan")
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if _, err := s2.RecordScan(ctx, "example.com", "", []store.Finding{finding("xampl.com")}); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}

	// Someone force-cleared the lock and wrote: the stored ETag moves on.
	obj := fake.Objects[dbKey]
	obj.ETag = `"etag-hijacked"`
	fake.Objects[dbKey] = obj

	err = s2.Close()
	if err == nil {
		t.Fatal("Close succeeded despite the remote changing under the held lock")
	}
	if !strings.Contains(err.Error(), "changed while this command held the lock") {
		t.Errorf("Close error does not name the conflict: %v", err)
	}
}

func TestOpenRejectsBadURLs(t *testing.T) {
	for _, raw := range []string{"s3://", "s3://bucket", "s3://bucket/", "s3:///key"} {
		if _, err := remote.Open(context.Background(), remotetest.NewFake(), raw, "scan"); err == nil {
			t.Errorf("Open(%q) succeeded, want error", raw)
		}
	}
}
