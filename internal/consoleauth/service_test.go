package consoleauth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/auth"
	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"golang.org/x/crypto/argon2"
)

func testConsoleConfig() config.ConsoleConfig {
	return config.ConsoleConfig{
		DevelopmentInsecure: true,
		SessionMaxAge:       8 * time.Hour,
		SessionIdleTimeout:  30 * time.Minute,
		CleanupInterval:     10 * time.Minute,
		LoginWindow:         15 * time.Minute,
		LoginMaxFailures:    5,
	}
}

// testPasswordHash builds a valid Argon2id PHC string with parameters inside the
// accepted limits so tests never depend on a precomputed fixture.
func testPasswordHash(t *testing.T, password string) string {
	t.Helper()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	hash := argon2.IDKey([]byte(password), salt, 2, 19456, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=19456,t=2,p=1$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash))
}

func TestNewRequiresDatabaseAndUsers(t *testing.T) {
	cfg := testConsoleConfig()
	if _, err := New(nil, cfg, map[string]User{"alice": {ID: "alice"}}); err == nil {
		t.Fatal("New with a nil database must fail")
	}
	if _, err := New(&database.DB{}, cfg, nil); err == nil {
		t.Fatal("New without users must fail")
	}
	if _, err := New(&database.DB{}, cfg, map[string]User{}); err == nil {
		t.Fatal("New with an empty user set must fail")
	}
	if service, err := New(&database.DB{}, cfg, map[string]User{"alice": {ID: "alice", Role: auth.ConsoleViewer}}); err != nil || service == nil {
		t.Fatalf("New with valid inputs failed: %v", err)
	}
}

func TestVerifyPasswordMatchesOnlyTheExactPassword(t *testing.T) {
	encoded := testPasswordHash(t, "correct horse battery staple")
	if !VerifyPassword(encoded, "correct horse battery staple") {
		t.Fatal("the exact password must verify")
	}
	if VerifyPassword(encoded, "wrong password") {
		t.Fatal("a different password must not verify")
	}
	if VerifyPassword("not-a-phc-string", "anything") {
		t.Fatal("a malformed hash must never authenticate")
	}
}

func TestParseArgon2idRejectsUnsafeOrMalformedEncodings(t *testing.T) {
	longSalt := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	longHash := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	cases := []string{
		"",
		"$argon2i$v=19$m=65536,t=2,p=1$" + longSalt + "$" + longHash,             // wrong algorithm
		"$argon2id$v=18$m=65536,t=2,p=1$" + longSalt + "$" + longHash,            // wrong version
		"$argon2id$v=19$m=1000,t=2,p=1$" + longSalt + "$" + longHash,             // memory below floor
		"$argon2id$v=19$m=99999999,t=2,p=1$" + longSalt + "$" + longHash,         // memory above ceiling
		"$argon2id$v=19$m=65536,t=1,p=1$" + longSalt + "$" + longHash,            // time below floor
		"$argon2id$v=19$m=65536,t=11,p=1$" + longSalt + "$" + longHash,           // time above ceiling
		"$argon2id$v=19$m=65536,t=2,p=0$" + longSalt + "$" + longHash,            // zero parallelism
		"$argon2id$v=19$m=65536,t=2,p=99$" + longSalt + "$" + longHash,           // parallelism above ceiling
		"$argon2id$v=19$m=65536,t=2,p=1$c2FsdA$" + longHash,                      // salt too short
		"$argon2id$v=19$m=65536,t=2,p=1$" + longSalt + "$c2FsdA",                 // hash too short
		"$argon2id$v=19$m=65536,t=2,p=1$" + longSalt + "$" + longHash + "$extra", // too many parts
	}
	for _, encoded := range cases {
		if _, _, _, err := parseArgon2id(encoded); err == nil {
			t.Errorf("expected parseArgon2id to reject %q", encoded)
		}
	}
}

func TestLoginRateLimitWindowTracksFailuresPerUserAndAddress(t *testing.T) {
	cfg := testConsoleConfig()
	service, err := New(&database.DB{}, cfg, map[string]User{"alice": {ID: "alice"}})
	if err != nil {
		t.Fatal(err)
	}
	key := "alice\x00127.0.0.1"
	now := time.Unix(1_700_000_000, 0)

	for i := 0; i < cfg.LoginMaxFailures; i++ {
		service.recordFailure(key, now)
	}
	if !service.isLimited(key, now) {
		t.Fatal("expected the key to be limited once max failures is reached")
	}
	if !service.isLimited(key, now.Add(cfg.LoginWindow-time.Second)) {
		t.Fatal("expected the key to stay limited inside the window")
	}
	if service.isLimited(key, now.Add(cfg.LoginWindow)) {
		t.Fatal("expected the window to reset after it expires")
	}

	service.recordFailure(key, now.Add(cfg.LoginWindow))
	if service.isLimited(key, now.Add(cfg.LoginWindow)) {
		t.Fatal("a single failure after reset must not trigger the limit")
	}

	other := "bob\x00127.0.0.1"
	if service.isLimited(other, now) {
		t.Fatal("a different user must not share the limit window")
	}
}

func TestClearFailuresRemovesTheRateLimit(t *testing.T) {
	cfg := testConsoleConfig()
	service, err := New(&database.DB{}, cfg, map[string]User{"alice": {ID: "alice"}})
	if err != nil {
		t.Fatal(err)
	}
	key := "alice\x0010.0.0.1"
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < cfg.LoginMaxFailures; i++ {
		service.recordFailure(key, now)
	}
	if !service.isLimited(key, now) {
		t.Fatal("setup: expected the key to be limited")
	}
	service.clearFailures(key)
	if service.isLimited(key, now) {
		t.Fatal("a successful login must clear the failure window")
	}
}

func TestNormalizedAddressExtractsAUsableIP(t *testing.T) {
	cases := []struct{ in, want string }{
		{"10.0.0.1:8080", "10.0.0.1"},
		{"10.0.0.1", "10.0.0.1"},
		{"[2001:db8::1]:443", "2001:db8::1"},
		{"2001:db8::1", "2001:db8::1"},
		{"", "127.0.0.1"},
		{"not-an-address", "127.0.0.1"},
		{"127.0.0.1:0", "127.0.0.1"},
	}
	for _, c := range cases {
		if got := normalizedAddress(c.in); got != c.want {
			t.Errorf("normalizedAddress(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestIntervalLiteralUsesSeconds(t *testing.T) {
	if got := intervalLiteral(30 * time.Minute); got != "1800.000000 seconds" {
		t.Errorf("intervalLiteral(30m)=%q want %q", got, "1800.000000 seconds")
	}
}

func TestCookieHelpersEnforceSecurityAttributes(t *testing.T) {
	service, err := New(&database.DB{}, testConsoleConfig(), map[string]User{"alice": {ID: "alice"}})
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour)

	session := service.Cookie(SessionCookieName, "token", true, expires)
	if !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Path != "/" {
		t.Fatalf("session cookie security attributes wrong: %+v", session)
	}
	if session.Secure {
		t.Fatal("development_insecure=true must clear the Secure flag")
	}
	if session.Expires.Before(time.Now()) {
		t.Fatal("session cookie must carry the expiry")
	}

	insecureDisabled := config.ConsoleConfig{DevelopmentInsecure: false}
	hardened, err := New(&database.DB{}, insecureDisabled, map[string]User{"alice": {ID: "alice"}})
	if err != nil {
		t.Fatal(err)
	}
	secureCookie := hardened.Cookie(SessionCookieName, "token", false, expires)
	if !secureCookie.Secure {
		t.Fatal("production cookies must set Secure")
	}

	expired := hardened.ExpiredCookie(SessionCookieName, true)
	if expired.MaxAge != -1 || expired.Value != "" || !expired.Expires.Before(time.Now()) {
		t.Fatalf("expired cookie must clear the browser value: %+v", expired)
	}
}
