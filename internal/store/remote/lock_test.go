package remote_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/smithy-go"

	"github.com/andoniaf/yatt/internal/store/remote"
	"github.com/andoniaf/yatt/internal/store/remote/remotetest"
)

func TestSecondOpenAbortsWithTheHolder(t *testing.T) {
	fake := remotetest.NewFake()
	ctx := context.Background()

	s, err := remote.Open(ctx, fake, dbURL, "scan")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	_, err = remote.Open(ctx, fake, dbURL, "history")
	if err == nil {
		t.Fatal("second Open succeeded while the lock was held")
	}
	var held *remote.ErrLockHeld
	if !errors.As(err, &held) {
		t.Fatalf("second Open error = %v, want ErrLockHeld", err)
	}

	// The message must identify the holder well enough to judge staleness, and
	// hand over the recovery command.
	hostname, _ := os.Hostname()
	msg := err.Error()
	for _, want := range []string{
		hostname,
		"pid " + strconv.Itoa(os.Getpid()),
		`operation "scan"`,
		"yatt state unlock --db " + dbURL,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("lock-held error missing %q: %s", want, msg)
		}
	}
}

func TestReadLockReportsTheHolder(t *testing.T) {
	fake := remotetest.NewFake()
	ctx := context.Background()

	s, err := remote.Open(ctx, fake, dbURL, "scan")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	info, err := remote.ReadLock(ctx, fake, dbURL)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if info.Operation != "scan" || info.PID != os.Getpid() {
		t.Errorf("ReadLock = %+v, want this process's scan lock", info)
	}
}

func TestReadLockWithoutALock(t *testing.T) {
	_, err := remote.ReadLock(context.Background(), remotetest.NewFake(), dbURL)
	if !errors.Is(err, remote.ErrNoLock) {
		t.Errorf("ReadLock on nothing = %v, want ErrNoLock", err)
	}
}

func TestUnlockClearsTheLock(t *testing.T) {
	fake := remotetest.NewFake()
	ctx := context.Background()

	s, err := remote.Open(ctx, fake, dbURL, "scan")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// The holder is deliberately not closed: this simulates the crashed
	// process whose lock Unlock exists to clear.
	t.Cleanup(func() { _ = s.Close() })

	info, err := remote.ReadLock(ctx, fake, dbURL)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if err := remote.Unlock(ctx, fake, dbURL, info); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if _, ok := fake.Objects[lockKey]; ok {
		t.Errorf("lock object still present after Unlock")
	}

	if err := remote.Unlock(ctx, fake, dbURL, info); !errors.Is(err, remote.ErrNoLock) {
		t.Errorf("second Unlock = %v, want ErrNoLock", err)
	}
}

func TestAcquireReportsBusyWhenTheLockCycles(t *testing.T) {
	fake := remotetest.NewFake()
	// Every conditional put loses, yet no lock is ever there to read
	// afterwards — the shape of a lock cycling between short-lived commands.
	fake.FailPut[lockKey] = &smithy.GenericAPIError{Code: "PreconditionFailed"}

	_, err := remote.Open(context.Background(), fake, dbURL, "scan")
	if err == nil {
		t.Fatal("Open acquired a lock that always conflicts")
	}
	if !strings.Contains(err.Error(), "busy") {
		t.Errorf("cycling-lock error = %v, want a busy/retry message", err)
	}
	// A cycling lock is contention, not staleness: steering the user to
	// force-unlock here would have them clear a live process's lock.
	if strings.Contains(err.Error(), "state unlock") {
		t.Errorf("cycling-lock error steers to force-unlock: %v", err)
	}
}

func TestUnlockRefusesALockThatChangedHands(t *testing.T) {
	fake := remotetest.NewFake()
	ctx := context.Background()

	s, err := remote.Open(ctx, fake, dbURL, "scan")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	stale, err := remote.ReadLock(ctx, fake, dbURL)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}

	// While the operator deliberates, the holder finishes and a new live
	// process takes the lock.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := remote.Open(ctx, fake, dbURL, "triage")
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer func() { _ = s2.Close() }()

	err = remote.Unlock(ctx, fake, dbURL, stale)
	if err == nil || !strings.Contains(err.Error(), "changed hands") {
		t.Fatalf("Unlock with a superseded snapshot = %v, want a changed-hands refusal", err)
	}
	if _, ok := fake.Objects[lockKey]; !ok {
		t.Errorf("the live holder's lock was cleared anyway")
	}
}
