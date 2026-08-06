package consoleauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/auth"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
)

func newConsoleAuthEnvironment(t *testing.T) (*Service, *database.DB) {
	t.Helper()
	url := os.Getenv("DEVICE_FARM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("DEVICE_FARM_TEST_DATABASE_URL is not set")
	}
	db, err := database.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE
		device_console_sessions, device_audit_events RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	users := map[string]User{
		"alice": {ID: "alice", DisplayName: "Alice Admin", Role: auth.ConsoleAdmin, PasswordHash: testPasswordHash(t, "correct-horse")},
	}
	service, err := New(db, testConsoleConfig(), users)
	if err != nil {
		t.Fatal(err)
	}
	return service, db
}

func requestWithSessionCookie(t *testing.T, token string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, "http://console.test/api/v1/console/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	return request
}

func requestWithCSRF(t *testing.T, token, csrf, header string) *http.Request {
	t.Helper()
	request := requestWithSessionCookie(t, token)
	if csrf != "" {
		request.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrf})
	}
	if header != "" {
		request.Header.Set("X-CSRF-Token", header)
	}
	return request
}

func TestLoginCreatesSessionAndAuditEvent(t *testing.T) {
	service, db := newConsoleAuthEnvironment(t)
	ctx := context.Background()

	created, err := service.Login(ctx, "alice", "correct-horse", "10.0.0.1:8080")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if created.Token == "" || created.CSRFToken == "" {
		t.Fatal("login must return a session token and a CSRF token")
	}
	if created.Token == created.CSRFToken {
		t.Fatal("session token and CSRF token must be independent")
	}
	if created.View.User.ID != "alice" || created.View.User.Role != auth.ConsoleAdmin {
		t.Fatalf("unexpected session user view: %+v", created.View.User)
	}
	if !created.View.ExpiresAt.After(time.Now()) {
		t.Fatal("session must carry a future expiry")
	}

	tokenHash := sha256.Sum256([]byte(created.Token))
	var stored []byte
	if err := db.Pool().QueryRow(ctx, `SELECT token_hash FROM device_console_sessions WHERE user_id=$1`, "alice").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, tokenHash[:]) {
		t.Fatal("only the token hash may be stored, never the plaintext token")
	}

	var actorType, action string
	var summary map[string]any
	if err := db.Pool().QueryRow(ctx, `SELECT actor_type, action, summary FROM device_audit_events WHERE action='console.login'`).Scan(&actorType, &action, &summary); err != nil {
		t.Fatalf("login audit event missing: %v", err)
	}
	if actorType != "console" || action != "console.login" {
		t.Fatalf("audit actor/action = %s/%s want console/console.login", actorType, action)
	}
	if summary["source_address"] != "10.0.0.1" {
		t.Fatalf("audit source_address = %v want 10.0.0.1", summary["source_address"])
	}
}

func TestLoginRejectsWrongPasswordWithoutCreatingSession(t *testing.T) {
	service, db := newConsoleAuthEnvironment(t)
	ctx := context.Background()

	if _, err := service.Login(ctx, "alice", "wrong-password", "10.0.0.1:8080"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password must fail with ErrInvalidCredentials, got %v", err)
	}
	var sessions int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM device_console_sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("failed login must not create a session, found %d", sessions)
	}
}

func TestLoginRateLimitsAfterRepeatedFailures(t *testing.T) {
	service, _ := newConsoleAuthEnvironment(t)
	ctx := context.Background()
	service.config.LoginMaxFailures = 3
	service.config.LoginWindow = time.Hour

	for i := 0; i < 2; i++ {
		if _, err := service.Login(ctx, "alice", "wrong-password", "10.0.0.1:8080"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: want ErrInvalidCredentials, got %v", i+1, err)
		}
	}
	if _, err := service.Login(ctx, "alice", "wrong-password", "10.0.0.1:8080"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("third failure must return ErrRateLimited, got %v", err)
	}
	if _, err := service.Login(ctx, "alice", "correct-horse", "10.0.0.1:8080"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("a correct password must still be rate limited, got %v", err)
	}
}

func TestAuthenticateAcceptsValidSessionAndRejectsRevoked(t *testing.T) {
	service, _ := newConsoleAuthEnvironment(t)
	ctx := context.Background()

	created, err := service.Login(ctx, "alice", "correct-horse", "10.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	request := requestWithSessionCookie(t, created.Token)
	principal, err := service.Authenticate(request)
	if err != nil {
		t.Fatalf("valid session rejected: %v", err)
	}
	if principal.SubjectID != "alice" || principal.Role != auth.RoleConsole || principal.ConsoleRole != auth.ConsoleAdmin {
		t.Fatalf("unexpected principal: %+v", principal)
	}

	if err := service.Logout(request); err != nil {
		t.Fatalf("logout failed: %v", err)
	}
	if _, err := service.Authenticate(request); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("revoked session must not authenticate, got %v", err)
	}
}

func TestAuthenticateRejectsMissingCookieExpiredAndIdleSessions(t *testing.T) {
	service, db := newConsoleAuthEnvironment(t)
	ctx := context.Background()

	if _, err := service.Authenticate(requestWithSessionCookie(t, "")); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("missing cookie must be unauthenticated, got %v", err)
	}

	created, err := service.Login(ctx, "alice", "correct-horse", "10.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte(created.Token))

	if _, err := db.Pool().Exec(ctx, `UPDATE device_console_sessions SET created_at=clock_timestamp()-interval '2 minutes',
		expires_at=clock_timestamp()-interval '1 minute' WHERE token_hash=$1`, tokenHash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(requestWithSessionCookie(t, created.Token)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired session must not authenticate, got %v", err)
	}

	renewed, err := service.Login(ctx, "alice", "correct-horse", "10.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	renewedHash := sha256.Sum256([]byte(renewed.Token))
	if _, err := db.Pool().Exec(ctx, `UPDATE device_console_sessions SET created_at=clock_timestamp()-interval '2 hours',
		last_seen_at=clock_timestamp()-interval '1 hour' WHERE token_hash=$1`, renewedHash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(requestWithSessionCookie(t, renewed.Token)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("idle-timeout session must not authenticate, got %v", err)
	}
}

func TestValidateCSRFRequiresCookieAndHeaderAgreement(t *testing.T) {
	service, _ := newConsoleAuthEnvironment(t)
	ctx := context.Background()

	created, err := service.Login(ctx, "alice", "correct-horse", "10.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}

	valid := requestWithCSRF(t, created.Token, created.CSRFToken, created.CSRFToken)
	if err := service.ValidateCSRF(valid, auth.Principal{}); err != nil {
		t.Fatalf("matching CSRF cookie and header must pass: %v", err)
	}

	badHeader := requestWithCSRF(t, created.Token, created.CSRFToken, "attacker-token")
	if err := service.ValidateCSRF(badHeader, auth.Principal{}); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("mismatched CSRF header must be rejected, got %v", err)
	}

	missingHeader := requestWithCSRF(t, created.Token, created.CSRFToken, "")
	if err := service.ValidateCSRF(missingHeader, auth.Principal{}); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("missing CSRF header must be rejected, got %v", err)
	}
}

func TestCurrentReturnsTheActiveSessionView(t *testing.T) {
	service, _ := newConsoleAuthEnvironment(t)
	ctx := context.Background()

	created, err := service.Login(ctx, "alice", "correct-horse", "10.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Current(requestWithSessionCookie(t, created.Token))
	if err != nil {
		t.Fatalf("current session lookup failed: %v", err)
	}
	if view.User.ID != "alice" || view.User.DisplayName != "Alice Admin" || view.User.Role != auth.ConsoleAdmin {
		t.Fatalf("unexpected session view: %+v", view)
	}
	if !view.ExpiresAt.Equal(created.View.ExpiresAt) {
		t.Fatalf("expiry mismatch: %v vs %v", view.ExpiresAt, created.View.ExpiresAt)
	}
}

func TestLogoutRequiresAnExistingSession(t *testing.T) {
	service, _ := newConsoleAuthEnvironment(t)

	if err := service.Logout(requestWithSessionCookie(t, "not-a-real-token")); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("logout without a session must fail, got %v", err)
	}
}
