package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/louiey-dev/my_assistant/db"
)

func TestLoginCurrentUserAndLogout(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, "file:auth-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(ctx, `
		INSERT INTO users(username, password_hash, role, created_at, updated_at)
		VALUES ('louie', ?, 'admin', '2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z')`, hash)
	if err != nil {
		t.Fatal(err)
	}

	service := New(database)
	service.SecureCookies = false
	service.Now = func() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) }
	handler := service.Handler()

	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"louie","password":"correct horse battery staple"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	var user User
	if err := json.NewDecoder(loginResponse.Body).Decode(&user); err != nil {
		t.Fatal(err)
	}
	if user.Username != "louie" || user.Role != "admin" {
		t.Fatalf("unexpected user: %+v", user)
	}
	cookie := loginResponse.Result().Cookies()[0]

	current := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	current.AddCookie(cookie)
	currentResponse := httptest.NewRecorder()
	handler.ServeHTTP(currentResponse, current)
	if currentResponse.Code != http.StatusOK {
		t.Fatalf("current-user status = %d, want %d", currentResponse.Code, http.StatusOK)
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.AddCookie(cookie)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", logoutResponse.Code, http.StatusNoContent)
	}

	currentAfterLogout := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	currentAfterLogout.AddCookie(cookie)
	afterLogoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(afterLogoutResponse, currentAfterLogout)
	if afterLogoutResponse.Code != http.StatusUnauthorized {
		t.Fatalf("current-user after logout status = %d, want %d", afterLogoutResponse.Code, http.StatusUnauthorized)
	}
}

func TestLoginRejectsInvalidPassword(t *testing.T) {
	database, err := db.Open(context.Background(), "file:auth-invalid-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if _, err := database.Exec("INSERT INTO users(username, password_hash, role, created_at, updated_at) VALUES ('user', ?, 'viewer', 'now', 'now')", hash); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"user","password":"wrong"}`))
	response := httptest.NewRecorder()
	New(database).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid password status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestLoginRateLimitsFailedAttempts(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, "file:auth-rate-limit-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO users(username, password_hash, role, created_at, updated_at) VALUES ('user', ?, 'viewer', 'now', 'now')", hash); err != nil {
		t.Fatal(err)
	}

	service := New(database)
	service.SecureCookies = false
	service.Now = func() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) }
	handler := service.Handler()
	for attempt := 0; attempt < maxLoginFailures; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"user","password":"wrong"}`))
		request.RemoteAddr = "192.0.2.1:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("failed attempt %d status = %d, want %d", attempt+1, response.Code, http.StatusUnauthorized)
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"user","password":"secret"}`))
	request.RemoteAddr = "192.0.2.1:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}
	if response.Header().Get("Retry-After") == "" {
		t.Fatal("rate-limited response missing Retry-After header")
	}
}

func TestRequireSessionProtectsCameraStream(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, "file:auth-camera-stream-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO users(username, password_hash, role, created_at, updated_at) VALUES ('viewer', 'unused', 'viewer', 'now', 'now')"); err != nil {
		t.Fatal(err)
	}

	service := New(database)
	service.SecureCookies = false
	stream := service.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	unauthenticated := httptest.NewRequest(http.MethodGet, "/api/v1/cameras/office/stream", nil)
	unauthenticatedResponse := httptest.NewRecorder()
	stream.ServeHTTP(unauthenticatedResponse, unauthenticated)
	if unauthenticatedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated stream status = %d, want %d", unauthenticatedResponse.Code, http.StatusUnauthorized)
	}

	token, err := service.newSession(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	authenticated := httptest.NewRequest(http.MethodGet, "/api/v1/cameras/office/stream", nil)
	authenticated.AddCookie(&http.Cookie{Name: service.cookieName(), Value: token})
	authenticatedResponse := httptest.NewRecorder()
	stream.ServeHTTP(authenticatedResponse, authenticated)
	if authenticatedResponse.Code != http.StatusOK {
		t.Fatalf("authenticated stream status = %d, want %d", authenticatedResponse.Code, http.StatusOK)
	}
}
