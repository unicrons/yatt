package remote_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andoniaf/yatt/internal/store/remote"
	"github.com/andoniaf/yatt/internal/store/remote/remotetest"
)

func writeLocalDB(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yatt.db")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPushThenPullRoundTrips(t *testing.T) {
	fake := remotetest.NewFake()
	ctx := context.Background()
	local := writeLocalDB(t, "local database bytes")

	if err := remote.Push(ctx, fake, local, dbURL, false); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !bytes.Equal(fake.Objects[dbKey].Body, []byte("local database bytes")) {
		t.Errorf("remote object does not match the pushed file")
	}
	if _, ok := fake.Objects[lockKey]; ok {
		t.Errorf("lock object still present after Push")
	}

	dest := filepath.Join(t.TempDir(), "copy.db")
	if err := remote.Pull(ctx, fake, dbURL, dest, false); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("local database bytes")) {
		t.Errorf("pulled file does not match the remote object")
	}
}

func TestPushRefusesAnExistingRemoteWithoutForce(t *testing.T) {
	fake := remotetest.NewFake()
	ctx := context.Background()
	local := writeLocalDB(t, "new bytes")

	if err := remote.Push(ctx, fake, writeLocalDB(t, "old bytes"), dbURL, false); err != nil {
		t.Fatalf("seeding Push: %v", err)
	}

	err := remote.Push(ctx, fake, local, dbURL, false)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("Push over an existing remote = %v, want a --force refusal", err)
	}
	if !bytes.Equal(fake.Objects[dbKey].Body, []byte("old bytes")) {
		t.Errorf("refused Push still changed the remote object")
	}

	if err := remote.Push(ctx, fake, local, dbURL, true); err != nil {
		t.Fatalf("Push --force: %v", err)
	}
	if !bytes.Equal(fake.Objects[dbKey].Body, []byte("new bytes")) {
		t.Errorf("forced Push did not replace the remote object")
	}
}

func TestPushRefusesALiveDatabase(t *testing.T) {
	local := writeLocalDB(t, "db")
	if err := os.WriteFile(local+"-wal", []byte("pending frames"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := remote.Push(context.Background(), remotetest.NewFake(), local, dbURL, false)
	if err == nil || !strings.Contains(err.Error(), "WAL") {
		t.Errorf("Push with a non-empty WAL sidecar = %v, want a refusal", err)
	}
}

func TestPullRefusesAnExistingLocalWithoutForce(t *testing.T) {
	fake := remotetest.NewFake()
	ctx := context.Background()

	if err := remote.Push(ctx, fake, writeLocalDB(t, "remote bytes"), dbURL, false); err != nil {
		t.Fatalf("seeding Push: %v", err)
	}

	dest := writeLocalDB(t, "precious local bytes")
	err := remote.Pull(ctx, fake, dbURL, dest, false)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("Pull onto an existing file = %v, want a --force refusal", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, []byte("precious local bytes")) {
		t.Errorf("refused Pull still overwrote the local file")
	}

	if err := remote.Pull(ctx, fake, dbURL, dest, true); err != nil {
		t.Fatalf("Pull --force: %v", err)
	}
	got, _ = os.ReadFile(dest)
	if !bytes.Equal(got, []byte("remote bytes")) {
		t.Errorf("forced Pull did not overwrite the local file")
	}
}

func TestPullWithoutARemoteDatabase(t *testing.T) {
	err := remote.Pull(context.Background(), remotetest.NewFake(), dbURL,
		filepath.Join(t.TempDir(), "copy.db"), false)
	if err == nil || !strings.Contains(err.Error(), "no remote database") {
		t.Errorf("Pull with nothing remote = %v, want a clear error", err)
	}
}
