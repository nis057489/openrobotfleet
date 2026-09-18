package httpserver

import (
	"example.com/openrobot-fleet/internal/db"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuthenticationSessions(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "test-private-password")
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.SQL.Close()
	s := &Server{DB: database}
	protected := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	check := func(value string) int {
		r := httptest.NewRequest("GET", "/api/robots", nil)
		if value != "" {
			r.AddCookie(&http.Cookie{Name: "auth_token", Value: value})
		}
		w := httptest.NewRecorder()
		protected.ServeHTTP(w, r)
		return w.Code
	}
	for _, token := range []string{"", "secret-admin-token", "invented"} {
		if got := check(token); got != 401 {
			t.Fatalf("forged cookie accepted: %d", got)
		}
	}
	login := func(password string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.handleLogin(w, httptest.NewRequest("POST", "https://fleet/api/login", strings.NewReader(`{"password":"`+password+`"}`)))
		return w
	}
	if got := login("incorrect").Code; got != 401 {
		t.Fatalf("wrong password: %d", got)
	}
	first, second := login("test-private-password"), login("test-private-password")
	if first.Code != 200 || second.Code != 200 {
		t.Fatalf("login failed: %d %d", first.Code, second.Code)
	}
	a, b := first.Result().Cookies()[0], second.Result().Cookies()[0]
	if a.Value == b.Value || len(a.Value) < 32 {
		t.Fatal("session tokens are not independent")
	}
	if !a.HttpOnly || !a.Secure || a.SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie protections missing")
	}
	if got := check(a.Value); got != 204 {
		t.Fatalf("valid session rejected: %d", got)
	}
	s.sessions.mu.Lock()
	s.sessions.tokens[a.Value] = time.Now().Add(-time.Second)
	s.sessions.mu.Unlock()
	if got := check(a.Value); got != 401 {
		t.Fatal("expired session accepted")
	}
	if got := check(b.Value); got != 204 {
		t.Fatal("unrelated session invalidated")
	}
	t.Setenv("ADMIN_PASSWORD", "")
	if got := login("").Code; got != 503 {
		t.Fatalf("unconfigured password: %d", got)
	}
}
