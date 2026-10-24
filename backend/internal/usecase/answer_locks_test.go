package usecase

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Answers of different readers or to different facts never wait for each
// other, however many are graded at once, and a released lock is forgotten.
func TestAnswerLocksKeepKeysApart(t *testing.T) {
	var locks answerLocks
	user := uuid.New()
	done := make(chan []func())
	go func() {
		var unlocks []func()
		for range 1000 {
			unlocks = append(unlocks, locks.lock(user, pgtype.UUID{Bytes: uuid.New(), Valid: true}))
		}
		done <- unlocks
	}()
	select {
	case unlocks := <-done:
		for _, unlock := range unlocks {
			unlock()
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a lock of one fact waited for another fact")
	}
	if n := len(locks.held); n != 0 {
		t.Fatalf("%d released locks are still kept", n)
	}
}

// The same reader and fact wait for each other.
func TestAnswerLocksSerializeOneKey(t *testing.T) {
	var locks answerLocks
	user, fact := uuid.New(), pgtype.UUID{Bytes: uuid.New(), Valid: true}
	unlock := locks.lock(user, fact)
	got := make(chan struct{})
	go func() {
		locks.lock(user, fact)()
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("the second answer did not wait for the first")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("the second answer was not let in after the first")
	}
}

// The floor of a moment sorts above ids made before it and at or below ids
// made from it on, as the database orders its UUIDv7 answer ids.
func TestUUIDV7Floor(t *testing.T) {
	before := uuid.Must(uuid.NewV7())
	time.Sleep(2 * time.Millisecond)
	now := time.Now()
	floor := uuidV7Floor(now)
	time.Sleep(2 * time.Millisecond)
	after := uuid.Must(uuid.NewV7())
	if floor.Version() != 7 || floor.Variant() != uuid.RFC4122 {
		t.Fatalf("floor %s is not a UUIDv7", floor)
	}
	if bytes.Compare(before[:], floor[:]) >= 0 || bytes.Compare(floor[:], after[:]) > 0 {
		t.Fatalf("floor %s does not sit between %s and %s", floor, before, after)
	}
	if sec, nsec := floor.Time().UnixTime(); time.Unix(sec, nsec).UnixMilli() != now.UnixMilli() {
		t.Fatalf("floor holds %v, want %v", time.Unix(sec, nsec), now)
	}
}
