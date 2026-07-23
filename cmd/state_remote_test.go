package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unicrons/yatt/internal/store/remote"
	"github.com/unicrons/yatt/internal/store/remote/remotetest"
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
	newRemoteClient = func(context.Context, string) (remote.Client, error) { return fake, nil }
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

func TestScanSurfacesAFailedUpload(t *testing.T) {
	fake := withFakeRemote(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"xample.com": true}})

	// The upload inside Close is the run's write path: its failure must fail
	// the command and carry the recovery instructions, not vanish into a
	// discarded deferred error.
	fake.FailPut[remoteDBKey] = errors.New("scripted upload failure")
	_, _, err := run(t, "scan", "example.com", "--db", remoteDBURL)
	if err == nil {
		t.Fatal("scan exited clean although its upload failed")
	}
	for _, want := range []string{"scripted upload failure", "state push", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("upload-failure error missing %q: %v", want, err)
		}
	}
}

// runWithInput is run with a scripted stdin, for commands that prompt.
func runWithInput(t *testing.T, input string, args ...string) (string, string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetIn(strings.NewReader(input))
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func seedLock(t *testing.T, fake *remotetest.Fake) {
	t.Helper()
	if _, err := remote.Open(context.Background(), fake, remoteDBURL, "scan"); err != nil {
		t.Fatalf("seeding the lock: %v", err)
	}
}

func TestStateUnlockAsksBeforeClearing(t *testing.T) {
	fake := withFakeRemote(t)
	seedLock(t, fake)

	// A declined prompt leaves the lock alone.
	stdout, _, err := runWithInput(t, "n\n", "state", "unlock", "--db", remoteDBURL)
	if err != nil {
		t.Fatalf("declined unlock: %v", err)
	}
	if _, ok := fake.Objects[remoteLockKey]; !ok {
		t.Fatalf("declined unlock still cleared the lock")
	}
	if !strings.Contains(stdout, `operation "scan"`) {
		t.Errorf("unlock did not show the holder before asking:\n%s", stdout)
	}

	stdout, _, err = runWithInput(t, "y\n", "state", "unlock", "--db", remoteDBURL)
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if _, ok := fake.Objects[remoteLockKey]; ok {
		t.Errorf("lock still present after a confirmed unlock:\n%s", stdout)
	}
}

func TestStateUnlockForceSkipsThePrompt(t *testing.T) {
	fake := withFakeRemote(t)
	seedLock(t, fake)

	// No stdin at all: --force must not read one.
	stdout, _, err := run(t, "state", "unlock", "--force", "--db", remoteDBURL)
	if err != nil {
		t.Fatalf("unlock --force: %v", err)
	}
	if _, ok := fake.Objects[remoteLockKey]; ok {
		t.Errorf("lock still present after unlock --force:\n%s", stdout)
	}
}

func TestStateUnlockWithNothingHeld(t *testing.T) {
	withFakeRemote(t)

	stdout, _, err := run(t, "state", "unlock", "--db", remoteDBURL)
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if !strings.Contains(stdout, "no lock is held") {
		t.Errorf("unlock on nothing = %q, want a friendly no-op", stdout)
	}
}

func TestStatePushAndPull(t *testing.T) {
	fake := withFakeRemote(t)

	local := filepath.Join(t.TempDir(), "yatt.db")
	if err := os.WriteFile(local, []byte("database bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := run(t, "state", "push", local, "--db", remoteDBURL); err != nil {
		t.Fatalf("push: %v", err)
	}
	if !bytes.Equal(fake.Objects[remoteDBKey].Body, []byte("database bytes")) {
		t.Fatalf("push did not upload the local file")
	}

	// A second push must not clobber what is now the shared database.
	if _, _, err := run(t, "state", "push", local, "--db", remoteDBURL); err == nil ||
		!strings.Contains(err.Error(), "--force") {
		t.Errorf("second push = %v, want a --force refusal", err)
	}

	dest := filepath.Join(t.TempDir(), "copy.db")
	if _, _, err := run(t, "state", "pull", dest, "--db", remoteDBURL); err != nil {
		t.Fatalf("pull: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("database bytes")) {
		t.Errorf("pulled copy does not match the remote database")
	}
}

func TestStateCommandsRequireARemoteDatabase(t *testing.T) {
	withFakeRemote(t)

	for _, args := range [][]string{
		{"state", "unlock", "--db", "/tmp/yatt.db"},
		{"state", "push", "--db", ""},
		{"state", "pull", "copy.db", "--db", "yatt.db"},
	} {
		_, _, err := run(t, args...)
		if err == nil || !strings.Contains(err.Error(), "s3://") {
			t.Errorf("%v = %v, want an error demanding an s3:// --db", args, err)
		}
	}
}
