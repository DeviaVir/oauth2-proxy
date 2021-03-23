package persistence

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/oauth2-proxy/oauth2-proxy/v7/pkg/apis/sessions"
	"github.com/oauth2-proxy/oauth2-proxy/v7/pkg/cookies"
)

// Manager wraps a Store and handles the implementation details of the
// sessions.SessionStore with its use of session tickets
type Manager struct {
	Store         Store
	cookieBuilder cookies.Builder
}

// NewManager creates a Manager that can wrap a Store and manage the
// sessions.SessionStore implementation details
func NewManager(store Store, cookieBuilder cookies.Builder) *Manager {
	return &Manager{
		Store:         store,
		cookieBuilder: cookieBuilder,
	}
}

// Save saves a session in a persistent Store. Save will generate (or reuse an
// existing) ticket which manages unique per session encryption & retrieval
// from the persistent data store.
func (m *Manager) Save(rw http.ResponseWriter, req *http.Request, s *sessions.SessionState) error {
	if s.CreatedAt == nil || s.CreatedAt.IsZero() {
		now := time.Now()
		s.CreatedAt = &now
	}

	tckt, err := decodeTicketFromRequest(req, m.cookieBuilder)
	if err != nil {
		tckt, err = newTicket(m.cookieBuilder)
		if err != nil {
			return fmt.Errorf("error creating a session ticket: %v", err)
		}
	}

	err = tckt.saveSession(s, func(key string, val []byte, exp time.Duration) error {
		return m.Store.Save(req.Context(), key, val, exp)
	})
	if err != nil {
		return err
	}

	return tckt.setCookie(rw, req, s)
}

// Load reads sessions.SessionState information from a session store. It will
// use the session ticket from the http.Request's cookie.
func (m *Manager) Load(req *http.Request) (*sessions.SessionState, error) {
	tckt, err := decodeTicketFromRequest(req, m.cookieBuilder)
	if err != nil {
		return nil, err
	}

	return tckt.loadSession(func(key string) ([]byte, error) {
		return m.Store.Load(req.Context(), key)
	})
}

// Clear clears any saved session information for a given ticket cookie.
// Then it clears all session data for that ticket in the Store.
func (m *Manager) Clear(rw http.ResponseWriter, req *http.Request) error {
	tckt, err := decodeTicketFromRequest(req, m.cookieBuilder)
	if err != nil {
		// Always clear the cookie, even when we can't load a cookie from
		// the request
		tckt = &ticket{
			cookieBuilder: m.cookieBuilder,
		}
		if err := tckt.clearCookie(rw, req); err != nil {
			return fmt.Errorf("error creating cookie to clear session: %v", err)
		}
		// Don't raise an error if we didn't have a Cookie
		if errors.Is(err, http.ErrNoCookie) {
			return nil
		}
		return fmt.Errorf("error decoding ticket to clear session: %v", err)
	}

	tckt.clearCookie(rw, req)
	return tckt.clearSession(func(key string) error {
		return m.Store.Clear(req.Context(), key)
	})
}
