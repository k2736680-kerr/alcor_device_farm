package consoleauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/auth"
	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/jackc/pgx/v5"
)

const (
	SessionCookieName = "device_farm_session"
	CSRFCookieName    = "device_farm_csrf"
)

var (
	ErrInvalidCredentials = errors.New("invalid console credentials")
	ErrRateLimited        = errors.New("console login rate limited")
	ErrUnauthenticated    = errors.New("console session is not authenticated")
)

type UserView struct {
	ID          string           `json:"id"`
	DisplayName string           `json:"display_name"`
	Role        auth.ConsoleRole `json:"role"`
}

type SessionView struct {
	User      UserView  `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
}

type CreatedSession struct {
	View      SessionView
	Token     string
	CSRFToken string
}

type failureWindow struct {
	startedAt time.Time
	count     int
}

type Service struct {
	db       *database.DB
	config   config.ConsoleConfig
	users    map[string]User
	mu       sync.Mutex
	failures map[string]failureWindow
}

func New(db *database.DB, cfg config.ConsoleConfig, users map[string]User) (*Service, error) {
	if db == nil {
		return nil, errors.New("console session database is required")
	}
	if len(users) == 0 {
		return nil, errors.New("console users are required")
	}
	return &Service{db: db, config: cfg, users: users, failures: map[string]failureWindow{}}, nil
}

func (service *Service) Login(ctx context.Context, userID, password, sourceAddress string) (CreatedSession, error) {
	userID = strings.TrimSpace(userID)
	sourceAddress = normalizedAddress(sourceAddress)
	key := strings.ToLower(userID) + "\x00" + sourceAddress
	if service.isLimited(key, time.Now()) {
		return CreatedSession{}, ErrRateLimited
	}
	user, exists := service.users[userID]
	valid := exists && VerifyPassword(user.PasswordHash, password)
	if !valid {
		if service.recordFailure(key, time.Now()) {
			return CreatedSession{}, ErrRateLimited
		}
		return CreatedSession{}, ErrInvalidCredentials
	}
	service.clearFailures(key)

	id, err := identifier.New()
	if err != nil {
		return CreatedSession{}, fmt.Errorf("generate console session ID: %w", err)
	}
	token, tokenHash, err := randomToken()
	if err != nil {
		return CreatedSession{}, err
	}
	csrfToken, csrfHash, err := randomToken()
	if err != nil {
		return CreatedSession{}, err
	}
	var createdAt, expiresAt time.Time
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO device_console_sessions
			(id,token_hash,user_id,display_name,role,csrf_hash,source_address,expires_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,clock_timestamp()+$8::interval)
			RETURNING created_at,expires_at`, id, tokenHash[:], user.ID, user.DisplayName, user.Role,
			csrfHash[:], sourceAddress, intervalLiteral(service.config.SessionMaxAge)).Scan(&createdAt, &expiresAt); err != nil {
			return err
		}
		auditID, err := identifier.New()
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO device_audit_events
			(id,actor_type,actor_id,action,resource_type,resource_id,request_id,summary)
			VALUES($1,'console',$2,'console.login','console_session',$3,$3,
			jsonb_build_object('source_address',$4::text))`, auditID, user.ID, id, sourceAddress)
		return err
	})
	if err != nil {
		return CreatedSession{}, fmt.Errorf("create console session: %w", err)
	}
	_ = createdAt
	return CreatedSession{
		View:  SessionView{User: UserView{ID: user.ID, DisplayName: user.DisplayName, Role: user.Role}, ExpiresAt: expiresAt},
		Token: token, CSRFToken: csrfToken,
	}, nil
}

func (service *Service) Authenticate(request *http.Request) (auth.Principal, error) {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return auth.Principal{}, ErrUnauthenticated
	}
	tokenHash := sha256.Sum256([]byte(cookie.Value))
	var sessionID, userID, displayName string
	var role auth.ConsoleRole
	err = service.db.Pool().QueryRow(request.Context(), `UPDATE device_console_sessions SET last_seen_at=clock_timestamp()
		WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp()
		AND last_seen_at>clock_timestamp()-$2::interval
		RETURNING id,user_id,display_name,role`, tokenHash[:], intervalLiteral(service.config.SessionIdleTimeout)).
		Scan(&sessionID, &userID, &displayName, &role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.Principal{}, ErrUnauthenticated
		}
		return auth.Principal{}, fmt.Errorf("authenticate console session: %w", err)
	}
	user, ok := service.users[userID]
	if !ok || user.DisplayName != displayName || user.Role != role {
		_, _ = service.db.Pool().Exec(request.Context(), `UPDATE device_console_sessions SET revoked_at=clock_timestamp() WHERE id=$1 AND revoked_at IS NULL`, sessionID)
		return auth.Principal{}, ErrUnauthenticated
	}
	return auth.Principal{Role: auth.RoleConsole, SubjectID: user.ID, DisplayName: user.DisplayName, ConsoleRole: user.Role}, nil
}

func (service *Service) ValidateCSRF(request *http.Request, _ auth.Principal) error {
	sessionCookie, sessionErr := request.Cookie(SessionCookieName)
	csrfCookie, csrfErr := request.Cookie(CSRFCookieName)
	header := request.Header.Get("X-CSRF-Token")
	if sessionErr != nil || csrfErr != nil || header == "" || csrfCookie.Value == "" || header != csrfCookie.Value {
		return ErrUnauthenticated
	}
	sessionHash := sha256.Sum256([]byte(sessionCookie.Value))
	csrfHash := sha256.Sum256([]byte(header))
	var valid bool
	err := service.db.Pool().QueryRow(request.Context(), `SELECT EXISTS(
		SELECT 1 FROM device_console_sessions WHERE token_hash=$1 AND csrf_hash=$2
		AND revoked_at IS NULL AND expires_at>clock_timestamp()
		AND last_seen_at>clock_timestamp()-$3::interval)`, sessionHash[:], csrfHash[:], intervalLiteral(service.config.SessionIdleTimeout)).Scan(&valid)
	if err != nil || !valid {
		return ErrUnauthenticated
	}
	return nil
}

func (service *Service) Current(request *http.Request) (SessionView, error) {
	principal, err := service.Authenticate(request)
	if err != nil {
		return SessionView{}, err
	}
	cookie, _ := request.Cookie(SessionCookieName)
	tokenHash := sha256.Sum256([]byte(cookie.Value))
	var expiresAt time.Time
	if err := service.db.Pool().QueryRow(request.Context(), `SELECT expires_at FROM device_console_sessions WHERE token_hash=$1`, tokenHash[:]).Scan(&expiresAt); err != nil {
		return SessionView{}, err
	}
	return SessionView{User: UserView{ID: principal.SubjectID, DisplayName: principal.DisplayName, Role: principal.ConsoleRole}, ExpiresAt: expiresAt}, nil
}

func (service *Service) Logout(request *http.Request) error {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil {
		return ErrUnauthenticated
	}
	tokenHash := sha256.Sum256([]byte(cookie.Value))
	result, err := service.db.Pool().Exec(request.Context(), `UPDATE device_console_sessions SET revoked_at=clock_timestamp()
		WHERE token_hash=$1 AND revoked_at IS NULL`, tokenHash[:])
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrUnauthenticated
	}
	return nil
}

func (service *Service) RunCleanup(ctx context.Context) {
	ticker := time.NewTicker(service.config.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = service.db.Pool().Exec(ctx, `DELETE FROM device_console_sessions
				WHERE expires_at<clock_timestamp()-interval '24 hours'
				OR (revoked_at IS NOT NULL AND revoked_at<clock_timestamp()-interval '24 hours')`)
		}
	}
}

func (service *Service) Cookie(name, value string, httpOnly bool, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: "/", Secure: !service.config.DevelopmentInsecure,
		HttpOnly: httpOnly, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds())}
}

func (service *Service) ExpiredCookie(name string, httpOnly bool) *http.Cookie {
	return &http.Cookie{Name: name, Value: "", Path: "/", Secure: !service.config.DevelopmentInsecure,
		HttpOnly: httpOnly, SameSite: http.SameSiteStrictMode, Expires: time.Unix(1, 0), MaxAge: -1}
}

func randomToken() (string, [32]byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", [32]byte{}, fmt.Errorf("generate random token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, sha256.Sum256([]byte(token)), nil
}

func normalizedAddress(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil && net.ParseIP(host) != nil {
		return host
	}
	if net.ParseIP(remoteAddress) != nil {
		return remoteAddress
	}
	return "127.0.0.1"
}

func intervalLiteral(value time.Duration) string { return fmt.Sprintf("%f seconds", value.Seconds()) }

func (service *Service) isLimited(key string, now time.Time) bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	window, ok := service.failures[key]
	if !ok || now.Sub(window.startedAt) >= service.config.LoginWindow {
		return false
	}
	return window.count >= service.config.LoginMaxFailures
}

func (service *Service) recordFailure(key string, now time.Time) bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	window, ok := service.failures[key]
	if !ok || now.Sub(window.startedAt) >= service.config.LoginWindow {
		window = failureWindow{startedAt: now}
	}
	window.count++
	service.failures[key] = window
	return window.count >= service.config.LoginMaxFailures
}

func (service *Service) clearFailures(key string) {
	service.mu.Lock()
	delete(service.failures, key)
	service.mu.Unlock()
}
