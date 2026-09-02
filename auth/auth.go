// Package auth provides HTTP handlers for dashboard authentication.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	defaultCookieName  = "my_assistant_session"
	defaultSessionTTL  = 24 * time.Hour
	maxPasswordBytes   = 72
	maxLoginFailures   = 5
	loginFailureWindow = 15 * time.Minute
	loginLockout       = time.Minute
)

type contextKey string

const userContextKey contextKey = "authenticated-user"

// User is the identity exposed to handlers and JSON responses.
type User struct {
	ID       int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Auth contains the dependencies and policy for dashboard authentication.
type Auth struct {
	DB            *sql.DB
	CookieName    string
	SessionTTL    time.Duration
	SecureCookies bool
	Now           func() time.Time
	Random        func([]byte) error
	limiter       *loginLimiter
	limiterMu     sync.Mutex
}

// New returns an Auth service with production-safe defaults. SecureCookies
// should only be disabled for local HTTP development and tests.
func New(database *sql.DB) *Auth {
	return &Auth{
		DB:            database,
		CookieName:    defaultCookieName,
		SessionTTL:    defaultSessionTTL,
		SecureCookies: true,
		Now:           time.Now,
		Random: func(buf []byte) error {
			_, err := rand.Read(buf)
			return err
		},
		limiter: newLoginLimiter(),
	}
}

// CreateUser creates an administrator account for the initial dashboard setup.
// Passwords are never stored in plaintext.
func CreateUser(ctx context.Context, database *sql.DB, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || len([]byte(password)) == 0 || len([]byte(password)) > maxPasswordBytes {
		return errors.New("username and password are required; password must be 1-72 bytes")
	}
	if database == nil {
		return errors.New("auth database is nil")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = database.ExecContext(ctx,
		"INSERT INTO users(username, password_hash, role, created_at, updated_at) VALUES (?, ?, 'admin', ?, ?)",
		username, string(hash), now, now,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return fmt.Errorf("username %q already exists", username)
		}
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// Handler returns the authentication routes.
func (a *Auth) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", a.login)
	mux.HandleFunc("/api/v1/auth/logout", a.logout)
	mux.HandleFunc("/api/v1/auth/me", a.me)
	return mux
}

// RequireSession rejects requests without a valid session and adds the user
// identity to the request context for authenticated handlers.
func (a *Auth) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.userFromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey, user)))
	})
}

// UserFromContext returns the authenticated user installed by
// RequireSession.
func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userContextKey).(User)
	return user, ok
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.Username) == "" || input.Password == "" || len([]byte(input.Password)) > maxPasswordBytes {
		writeError(w, http.StatusBadRequest, "invalid_request", "Username and password are required.")
		return
	}
	username := strings.TrimSpace(input.Username)
	if retryAfter, limited := a.loginRateLimit(r, username); limited {
		w.Header().Set("Retry-After", retryAfter)
		writeError(w, http.StatusTooManyRequests, "too_many_requests", "Too many failed login attempts. Try again later.")
		return
	}

	var user User
	var passwordHash string
	err := a.DB.QueryRowContext(r.Context(),
		"SELECT user_id, username, role, password_hash FROM users WHERE username = ?",
		username,
	).Scan(&user.ID, &user.Username, &user.Role, &passwordHash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(input.Password)) != nil {
		a.recordLoginFailure(r, username)
		writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid username or password.")
		return
	}
	a.resetLoginFailures(r, username)

	token, err := a.newSession(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Unable to create session.")
		return
	}
	http.SetCookie(w, a.sessionCookie(token))
	writeJSON(w, http.StatusOK, user)
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if cookie, err := r.Cookie(a.cookieName()); err == nil {
		_, _ = a.DB.ExecContext(r.Context(), "DELETE FROM sessions WHERE session_hash = ?", hashToken(cookie.Value))
	}
	http.SetCookie(w, a.expiredCookie())
	w.WriteHeader(http.StatusNoContent)
}

func (a *Auth) me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	user, err := a.userFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (a *Auth) newSession(ctx context.Context, userID int64) (string, error) {
	if a.DB == nil {
		return "", errors.New("auth database is nil")
	}
	buf := make([]byte, 32)
	if err := a.random()(buf); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	now := a.now().UTC()
	expires := now.Add(a.sessionTTL()).UTC()
	_, err := a.DB.ExecContext(ctx,
		"INSERT INTO sessions(session_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		hashToken(token), userID, now.Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano),
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (a *Auth) userFromRequest(r *http.Request) (User, error) {
	cookie, err := r.Cookie(a.cookieName())
	if err != nil || cookie.Value == "" || a.DB == nil {
		return User{}, errors.New("missing session")
	}
	var user User
	var expiresAt string
	err = a.DB.QueryRowContext(r.Context(), `
		SELECT u.user_id, u.username, u.role, s.expires_at
		FROM sessions s JOIN users u ON u.user_id = s.user_id
		WHERE s.session_hash = ?`, hashToken(cookie.Value),
	).Scan(&user.ID, &user.Username, &user.Role, &expiresAt)
	if err != nil {
		return User{}, err
	}
	expires, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil || !expires.After(a.now().UTC()) {
		_, _ = a.DB.ExecContext(r.Context(), "DELETE FROM sessions WHERE session_hash = ?", hashToken(cookie.Value))
		return User{}, errors.New("expired session")
	}
	return user, nil
}

func (a *Auth) sessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     a.cookieName(),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookies(),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(a.sessionTTL().Seconds()),
	}
}

func (a *Auth) expiredCookie() *http.Cookie {
	cookie := a.sessionCookie("")
	cookie.MaxAge = -1
	cookie.Expires = time.Unix(1, 0)
	return cookie
}

func (a *Auth) cookieName() string {
	if a.CookieName != "" {
		return a.CookieName
	}
	return defaultCookieName
}

func (a *Auth) sessionTTL() time.Duration {
	if a.SessionTTL > 0 {
		return a.SessionTTL
	}
	return defaultSessionTTL
}

func (a *Auth) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Auth) loginRateLimit(r *http.Request, username string) (string, bool) {
	limiter := a.loginLimiter()
	if limiter == nil {
		return "", false
	}
	remaining := limiter.remaining(a.loginKeys(r, username), a.now())
	if remaining <= 0 {
		return "", false
	}
	return strconv.Itoa(remaining), true
}

func (a *Auth) recordLoginFailure(r *http.Request, username string) {
	if limiter := a.loginLimiter(); limiter != nil {
		limiter.record(a.loginKeys(r, username), a.now())
	}
}

func (a *Auth) resetLoginFailures(r *http.Request, username string) {
	if limiter := a.loginLimiter(); limiter != nil {
		limiter.reset(a.loginKeys(r, username))
	}
}

func (a *Auth) loginLimiter() *loginLimiter {
	a.limiterMu.Lock()
	defer a.limiterMu.Unlock()
	if a.limiter == nil {
		a.limiter = newLoginLimiter()
	}
	return a.limiter
}

func (a *Auth) loginKeys(r *http.Request, username string) []string {
	return []string{"ip:" + clientIP(r), "username:" + strings.ToLower(username)}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

type loginAttempt struct {
	failures    int
	windowStart time.Time
	blockedTill time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]loginAttempt)}
}

func (l *loginLimiter) remaining(keys []string, now time.Time) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range keys {
		attempt, ok := l.attempts[key]
		if !ok {
			continue
		}
		if !attempt.blockedTill.IsZero() && now.Before(attempt.blockedTill) {
			return int(math.Ceil(attempt.blockedTill.Sub(now).Seconds()))
		}
		if now.Sub(attempt.windowStart) >= loginFailureWindow {
			delete(l.attempts, key)
		}
	}
	return 0
}

func (l *loginLimiter) record(keys []string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range keys {
		attempt := l.attempts[key]
		if attempt.windowStart.IsZero() || now.Sub(attempt.windowStart) >= loginFailureWindow {
			attempt = loginAttempt{windowStart: now}
		}
		attempt.failures++
		if attempt.failures >= maxLoginFailures {
			attempt.blockedTill = now.Add(loginLockout)
		}
		l.attempts[key] = attempt
	}
}

func (l *loginLimiter) reset(keys []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range keys {
		delete(l.attempts, key)
	}
}

func (a *Auth) random() func([]byte) error {
	if a.Random != nil {
		return a.Random
	}
	return func(buf []byte) error {
		_, err := rand.Read(buf)
		return err
	}
}

func (a *Auth) secureCookies() bool {
	return a.SecureCookies
}

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
