package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

type sessionStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time
}

func (s *sessionStore) create(now time.Time) (string, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(secret[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokens == nil {
		s.tokens = make(map[string]time.Time)
	}
	for key, expiry := range s.tokens {
		if !now.Before(expiry) {
			delete(s.tokens, key)
		}
	}
	s.tokens[token] = now.Add(24 * time.Hour)
	return token, nil
}

func (s *sessionStore) valid(token string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry, ok := s.tokens[token]
	if !ok || !now.Before(expiry) {
		delete(s.tokens, token)
		return false
	}
	return true
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isUpload := strings.HasPrefix(r.URL.Path, "/api/robots/") && strings.HasSuffix(r.URL.Path, "/upload")
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/login" || isUpload {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie("auth_token")
		if err != nil || !s.sessions.valid(cookie.Value, time.Now()) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
