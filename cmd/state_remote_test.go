package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/andoniaf/yatt/internal/store/remote"
	"github.com/andoniaf/yatt/internal/store/remote/remotetest"
)

const (
	remoteDBURL   = "s3://bucket/state/yatt.db"
	remoteDBKey   = "bucket/state/yatt.db"
	remoteLockKey = "bucket/state/yatt.db.lock"
)

// withFakeRemote swaps the S3 client constructor for an in-memory fake, so a
// command test exercises the full remote flow — lock, download, upload —
// without a network.
func withFakeRemote(t *testing.T) *remotetest.Fake {
	t.Helper()

	fake := remotetest.NewFake()
	original := newRemoteClient
	newRemoteClient = func(context.Context) (remote.Client, error) { return fake, nil }
	t.Cleanup(func() { newRemoteClient = original })
	return fake
}

func TestScanAndHistoryShareARemoteDatabase(t *testing.T) {
	fake := withFakeRemote(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"xample.com": true}})

	stdout, _, err := run(t, "scan", "example.com", "--db", remoteDBURL)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !strings.Contains(stdout, "xample.com") {
		t.Errorf("scan output missing the registered candidate:\n%s", stdout)
	}
	if _, ok := fake.Objects[remoteDBKey]; !ok {
		t.Fatalf("scan did not upload the database to %s", remoteDBKey)
	}
	if _, ok := fake.Objects[remoteLockKey]; ok {
		t.Errorf("scan left its lock behind")
	}

	// history is a separate invocation: everything it shows was downloaded.
	stdout, _, err = run(t, "history", "example.com", "--db", remoteDBURL)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	rows := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(rows) < 2 {
		t.Errorf("history over the remote database lists no scans:\n%s", stdout)
	}
}

func TestCommandsAbortWhenTheRemoteIsLocked(t *testing.T) {
	fake := withFakeRemote(t)
	withFakeResolver(t, scriptedResolver{})

	// A holder that never releases: a crashed process, from this side.
	if _, err := remote.Open(context.Background(), fake, remoteDBURL, "scan"); err != nil {
		t.Fatalf("seeding the lock: %v", err)
	}

	_, _, err := run(t, "scan", "example.com", "--db", remoteDBURL)
	if err == nil {
		t.Fatal("scan ran despite the lock being held")
	}
	for _, want := range []string{"locked", `operation "scan"`, "yatt state unlock"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("lock abort error missing %q: %v", want, err)
		}
	}
}
