package correlation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

const (
	HeaderRequestID = "X-Request-Id"
	HeaderRunID     = "X-Eval-Run-Id"
	HeaderAttemptID = "X-Eval-Attempt-Id"
	HeaderTrace     = "traceparent"
	maxHeaderLength = 256
)

type Values struct {
	RequestID string
	RunID     string
	AttemptID string
	Trace     string
}

type contextKey struct{}

var fallbackSequence atomic.Uint64

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		values := Values{
			RequestID: cleanHeader(request.Header.Get(HeaderRequestID)),
			RunID:     cleanHeader(request.Header.Get(HeaderRunID)),
			AttemptID: cleanHeader(request.Header.Get(HeaderAttemptID)),
			Trace:     cleanHeader(request.Header.Get(HeaderTrace)),
		}
		if values.RequestID == "" {
			values.RequestID = newRequestID()
		}

		writer.Header().Set(HeaderRequestID, values.RequestID)
		if values.RunID != "" {
			writer.Header().Set(HeaderRunID, values.RunID)
		}
		if values.AttemptID != "" {
			writer.Header().Set(HeaderAttemptID, values.AttemptID)
		}
		if values.Trace != "" {
			writer.Header().Set(HeaderTrace, values.Trace)
		}

		ctx := context.WithValue(request.Context(), contextKey{}, values)
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

func FromContext(ctx context.Context) Values {
	values, ok := ctx.Value(contextKey{}).(Values)
	if !ok {
		return Values{}
	}
	return values
}

func LogAttrs(ctx context.Context) []any {
	values := FromContext(ctx)
	attrs := []any{slog.String("request_id", values.RequestID)}
	if values.RunID != "" {
		attrs = append(attrs, slog.String("eval_run_id", values.RunID))
	}
	if values.AttemptID != "" {
		attrs = append(attrs, slog.String("eval_attempt_id", values.AttemptID))
	}
	if values.Trace != "" {
		attrs = append(attrs, slog.String("traceparent", values.Trace))
	}
	return attrs
}

func newRequestID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("req_fallback_%d_%d", time.Now().UnixNano(), fallbackSequence.Add(1))
	}
	return "req_" + hex.EncodeToString(buffer)
}

func cleanHeader(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxHeaderLength {
		return ""
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return ""
		}
	}
	return value
}
