package booking

import (
	"context"
	"sync"
)

// mockStore is a hand-rolled test double for the BookingStore interface.
// It records method calls so tests can assert delegation, and its
// configurable errors let tests verify error propagation — all without Redis.
type mockStore struct {
	mu sync.Mutex

	bookings   map[string]Booking
	bookErr    error
	confirmErr error
	releaseErr error

	booked      []Booking
	confirmed   []string // session IDs
	released    []string // session IDs
	confirmUIDs []string
	releaseUIDs []string
}

func newMockStore() *mockStore {
	return &mockStore{bookings: map[string]Booking{}}
}

func (m *mockStore) Book(b Booking) (Booking, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.booked = append(m.booked, b)
	if m.bookErr != nil {
		return Booking{}, m.bookErr
	}
	if _, exists := m.bookings[b.SeatID]; exists {
		return Booking{}, ErrSeatAlreadyBooked
	}
	b.ID = "mock-session-id"
	b.Status = "held"
	m.bookings[b.SeatID] = b
	return b, nil
}

func (m *mockStore) ListBookings(movieID string) []Booking {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []Booking
	for _, b := range m.bookings {
		if b.MovieID == movieID {
			result = append(result, b)
		}
	}
	return result
}

func (m *mockStore) Confirm(_ context.Context, sessionID, userID string) (Booking, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.confirmed = append(m.confirmed, sessionID)
	m.confirmUIDs = append(m.confirmUIDs, userID)
	if m.confirmErr != nil {
		return Booking{}, m.confirmErr
	}
	b := Booking{ID: sessionID, UserID: userID, Status: "confirmed"}
	m.bookings[sessionID] = b
	return b, nil
}

func (m *mockStore) Release(_ context.Context, sessionID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.released = append(m.released, sessionID)
	m.releaseUIDs = append(m.releaseUIDs, userID)
	return m.releaseErr
}

// compile-time check: mockStore must always satisfy BookingStore.
var _ BookingStore = (*mockStore)(nil)
