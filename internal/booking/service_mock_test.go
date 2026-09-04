package booking

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// --- Service delegation (pure mock, no Redis) ---

func TestUnit_Service_Book_DelegatesToStore(t *testing.T) {
	store := newMockStore()
	svc := NewService(store)

	got, err := svc.Book(Booking{MovieID: "inception", SeatID: "A1", UserID: "u1"})
	if err != nil {
		t.Fatalf("Book() error = %v, want nil", err)
	}
	if len(store.booked) != 1 {
		t.Fatalf("store.Book called %d times, want 1", len(store.booked))
	}
	b := store.booked[0]
	if b.MovieID != "inception" || b.SeatID != "A1" || b.UserID != "u1" {
		t.Errorf("store received %+v, want movie=inception seat=A1 user=u1", b)
	}
	if got.ID != "mock-session-id" || got.Status != "held" {
		t.Errorf("Book() = %+v, want session id %q with status %q", got, "mock-session-id", "held")
	}
}

func TestUnit_Service_Book_PropagatesStoreError(t *testing.T) {
	store := newMockStore()
	store.bookErr = ErrSeatAlreadyBooked
	svc := NewService(store)

	if _, err := svc.Book(Booking{SeatID: "A1"}); !errors.Is(err, ErrSeatAlreadyBooked) {
		t.Errorf("Book() error = %v, want ErrSeatAlreadyBooked", err)
	}
}

func TestUnit_Service_ConfirmSeat_PassesSessionAndUser(t *testing.T) {
	store := newMockStore()
	svc := NewService(store)

	got, err := svc.ConfirmSeat(context.Background(), "sess-1", "u1")
	if err != nil {
		t.Fatalf("ConfirmSeat() error = %v, want nil", err)
	}
	if len(store.confirmed) != 1 || store.confirmed[0] != "sess-1" {
		t.Errorf("store confirmed = %v, want [sess-1]", store.confirmed)
	}
	if store.confirmUIDs[0] != "u1" {
		t.Errorf("store userID = %q, want u1", store.confirmUIDs[0])
	}
	if got.Status != "confirmed" {
		t.Errorf("status = %q, want confirmed", got.Status)
	}
}

func TestUnit_Service_ConfirmSeat_PropagatesError(t *testing.T) {
	store := newMockStore()
	store.confirmErr = errors.New("session not found")
	svc := NewService(store)

	if _, err := svc.ConfirmSeat(context.Background(), "missing", "u1"); err == nil {
		t.Error("ConfirmSeat() error = nil, want an error")
	}
}

func TestUnit_Service_ReleaseSeat_PassesSessionAndUser(t *testing.T) {
	store := newMockStore()
	svc := NewService(store)

	if err := svc.ReleaseSeat(context.Background(), "sess-2", "u2"); err != nil {
		t.Fatalf("ReleaseSeat() error = %v, want nil", err)
	}
	if len(store.released) != 1 || store.released[0] != "sess-2" || store.releaseUIDs[0] != "u2" {
		t.Errorf("store release args mismatch: sessions=%v users=%v", store.released, store.releaseUIDs)
	}
}

func TestUnit_Service_ReleaseSeat_PropagatesError(t *testing.T) {
	store := newMockStore()
	store.releaseErr = errors.New("boom")
	svc := NewService(store)

	if err := svc.ReleaseSeat(context.Background(), "s", "u"); err == nil {
		t.Error("ReleaseSeat() error = nil, want an error")
	}
}

// --- Redis store helpers (pure functions, no connection) ---

func TestUnit_SessionKey_Format(t *testing.T) {
	if got := sessionKey("abc-123"); got != "session:abc-123" {
		t.Errorf("sessionKey() = %q, want %q", got, "session:abc-123")
	}
}

func TestUnit_ParseSession_ValidJSON(t *testing.T) {
	raw := `{"ID":"s1","MovieID":"inception","SeatID":"B2","UserID":"u9","Status":"held"}`
	got, err := parseSession(raw)
	if err != nil {
		t.Fatalf("parseSession() error = %v, want nil", err)
	}
	want := Booking{ID: "s1", MovieID: "inception", SeatID: "B2", UserID: "u9", Status: "held"}
	if got != want {
		t.Errorf("parseSession() = %+v, want %+v", got, want)
	}
}

func TestUnit_ParseSession_InvalidJSON(t *testing.T) {
	if _, err := parseSession("not-json"); err == nil {
		t.Error("parseSession() error = nil, want an error for invalid JSON")
	}
}

// --- MemoryStore logic (in-memory, no Redis) ---

func TestUnit_MemoryStore_Book_RejectsDuplicateSeat(t *testing.T) {
	store := NewMemoryStore()

	if err := store.Book(Booking{SeatID: "A1", UserID: "u1"}); err != nil {
		t.Fatalf("first Book() error = %v, want nil", err)
	}
	if err := store.Book(Booking{SeatID: "A1", UserID: "u2"}); !errors.Is(err, ErrSeatAlreadyBooked) {
		t.Errorf("second Book() error = %v, want ErrSeatAlreadyBooked", err)
	}
}

func TestUnit_MemoryStore_ListBookings_FiltersByMovie(t *testing.T) {
	store := NewMemoryStore()
	_ = store.Book(Booking{MovieID: "inception", SeatID: "A1"})
	_ = store.Book(Booking{MovieID: "dune", SeatID: "A1"})

	got := store.ListBookings("inception")
	if len(got) != 1 || got[0].MovieID != "inception" {
		t.Errorf("ListBookings() = %+v, want only the inception booking", got)
	}
}

// --- ConcurrentStore logic (mutex-based, no Redis) ---

func TestUnit_ConcurrentStore_NoDoubleBooking(t *testing.T) {
	store := NewConcurentStore()

	const workers = 100
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.Book(Booking{SeatID: "A1", UserID: "u"})
		}()
	}
	wg.Wait()
	close(errs)

	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 success, got %d", successes)
	}
}

func TestUnit_ConcurrentStore_ListBookings_FiltersByMovie(t *testing.T) {
	store := NewConcurentStore()
	_ = store.Book(Booking{MovieID: "inception", SeatID: "A1"})
	_ = store.Book(Booking{MovieID: "dune", SeatID: "B1"})

	got := store.ListBookings("dune")
	if len(got) != 1 || got[0].MovieID != "dune" {
		t.Errorf("ListBookings() = %+v, want only the dune booking", got)
	}
}
