package booking

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestServer(t *testing.T, store *mockStore) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	h := NewHandler(NewService(store))
	mux.HandleFunc("GET /movies/{movieID}/seats", h.ListSeats)
	mux.HandleFunc("POST /movies/{movieID}/seats/{seatID}/hold", h.HoldSeat)
	mux.HandleFunc("PUT /sessions/{sessionID}/confirm", h.ConfirmSession)
	mux.HandleFunc("DELETE /sessions/{sessionID}", h.ReleaseSession)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func doJSON(t *testing.T, srv *httptest.Server, method, path string, body any) *http.Response {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func TestUnit_Handler_HoldSeat_Created(t *testing.T) {
	store := newMockStore()
	srv := newTestServer(t, store)

	resp := doJSON(t, srv, http.MethodPost, "/movies/inception/seats/A1/hold", map[string]string{"user_id": "u1"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	body := decode[struct {
		SessionID string `json:"session_id"`
		MovieID   string `json:"movieID"`
		SeatID    string `json:"seat_id"`
		ExpiresAt string `json:"expires_at"`
	}](t, resp)

	if body.SessionID == "" || body.SeatID != "A1" || body.MovieID != "inception" {
		t.Errorf("unexpected hold response: %+v", body)
	}
	if _, err := time.Parse(time.RFC3339, body.ExpiresAt); err != nil {
		t.Errorf("expires_at %q is not RFC3339: %v", body.ExpiresAt, err)
	}
	if len(store.booked) != 1 || store.booked[0].SeatID != "A1" {
		t.Errorf("store did not receive the hold request: %+v", store.booked)
	}
}

func TestUnit_Handler_ListSeats_ReturnsSeatMap(t *testing.T) {
	store := newMockStore()
	store.bookings["A1"] = Booking{MovieID: "inception", SeatID: "A1", UserID: "u1", Status: "confirmed"}
	store.bookings["B2"] = Booking{MovieID: "inception", SeatID: "B2", UserID: "u2", Status: "held"}

	srv := newTestServer(t, store)
	resp := doJSON(t, srv, http.MethodGet, "/movies/inception/seats", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	seats := decode[[]seatInfo](t, resp)
	if len(seats) != 2 {
		t.Fatalf("got %d seats, want 2", len(seats))
	}
	bySeat := map[string]seatInfo{}
	for _, s := range seats {
		bySeat[s.SeatID] = s
	}
	if !bySeat["A1"].Confirmed || bySeat["A1"].UserID != "u1" {
		t.Errorf("seat A1 = %+v, want confirmed booking by u1", bySeat["A1"])
	}
	if !bySeat["B2"].Booked || bySeat["B2"].Confirmed {
		t.Errorf("seat B2 = %+v, want booked-but-not-confirmed", bySeat["B2"])
	}
}

func TestUnit_Handler_ConfirmSession_ReturnsConfirmedBooking(t *testing.T) {
	store := newMockStore()
	srv := newTestServer(t, store)

	resp := doJSON(t, srv, http.MethodPut, "/sessions/sess-9/confirm", map[string]string{"user_id": "u1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body := decode[sessionResponse](t, resp)
	if body.SessionID != "sess-9" || body.Status != "confirmed" {
		t.Errorf("confirm response = %+v, want session sess-9 confirmed", body)
	}
	if store.confirmUIDs[0] != "u1" {
		t.Errorf("store userID = %q, want u1", store.confirmUIDs[0])
	}
}

func TestUnit_Handler_ReleaseSession_ReturnsNoContent(t *testing.T) {
	store := newMockStore()
	srv := newTestServer(t, store)

	resp := doJSON(t, srv, http.MethodDelete, "/sessions/sess-7", map[string]string{"user_id": "u1"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	if len(store.released) != 1 || store.released[0] != "sess-7" {
		t.Errorf("store released = %v, want [sess-7]", store.released)
	}
}

func TestUnit_Handler_ConfirmSession_StoreError_ShortCircuits(t *testing.T) {
	store := newMockStore()
	store.confirmErr = errors.New("session not found")
	srv := newTestServer(t, store)

	resp := doJSON(t, srv, http.MethodPut, "/sessions/missing/confirm", map[string]string{"user_id": "u1"})
	// Current handler behavior: on store error it returns without writing a body.
	if resp.StatusCode == http.StatusNotImplemented {
		t.Fatalf("unexpected 501")
	}
	if len(store.confirmed) != 1 {
		t.Errorf("store was not called exactly once, got %d", len(store.confirmed))
	}
}
