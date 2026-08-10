package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func requestFrom(remoteAddress string) *http.Request {
	request, err := http.NewRequest(http.MethodPost, "http://console.test/console/api/v1/sessions", nil)
	if err != nil {
		panic(err)
	}
	request.RemoteAddr = remoteAddress
	return request
}

func TestClientAddressTrustsForwardedForOnlyFromLoopbackPeer(t *testing.T) {
	request := requestFrom("127.0.0.1:55234")
	request.Header.Set("X-Forwarded-For", "10.20.30.40")
	if got := clientAddress(request); got != "10.20.30.40" {
		t.Fatalf("clientAddress = %q, want the forwarded address 10.20.30.40", got)
	}

	request.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1, 127.0.0.1")
	if got := clientAddress(request); got != "203.0.113.7" {
		t.Fatalf("clientAddress = %q, want the first forwarded entry", got)
	}
}

func TestClientAddressIgnoresForwardedForFromPrivatePeer(t *testing.T) {
	request := requestFrom("192.168.1.10:8080")
	request.Header.Set("X-Forwarded-For", "198.51.100.23")
	if got := clientAddress(request); got != "192.168.1.10" {
		t.Fatalf("clientAddress = %q, want the direct private peer", got)
	}
}

func TestClientAddressIgnoresForwardedForFromPublicPeer(t *testing.T) {
	request := requestFrom("203.0.113.99:4456")
	request.Header.Set("X-Forwarded-For", "6.6.6.6")
	if got := clientAddress(request); got != "203.0.113.99" {
		t.Fatalf("clientAddress = %q, want the direct peer (spoofed X-Forwarded-For must be ignored)", got)
	}
}

func TestClientAddressFallsBackToTheImmediatePeer(t *testing.T) {
	request := requestFrom("127.0.0.1:55234")
	if got := clientAddress(request); got != "127.0.0.1" {
		t.Fatalf("clientAddress = %q, want 127.0.0.1 without X-Forwarded-For", got)
	}

	invalid := requestFrom("127.0.0.1:55234")
	invalid.Header.Set("X-Forwarded-For", "not-an-ip")
	if got := clientAddress(invalid); got != "127.0.0.1" {
		t.Fatalf("clientAddress = %q, want the peer when the forwarded value is not an IP", got)
	}
}

func TestClientAddressHandlesIPv6AndMalformedPeers(t *testing.T) {
	request := requestFrom("[2001:db8::7]:443")
	if got := clientAddress(request); got != "2001:db8::7" {
		t.Fatalf("clientAddress = %q, want the IPv6 peer", got)
	}

	proxied := requestFrom("[::1]:443")
	proxied.Header.Set("X-Forwarded-For", "2001:db8::99")
	if got := clientAddress(proxied); got != "2001:db8::99" {
		t.Fatalf("clientAddress = %q, want the forwarded IPv6 address from a loopback proxy", got)
	}

	malformed := requestFrom("")
	if got := clientAddress(malformed); got != "127.0.0.1" {
		t.Fatalf("clientAddress = %q, want the 127.0.0.1 fallback for a malformed peer", got)
	}
}

func TestRemoteControlTimeoutReturnsStableRetryableError(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "http://console.test/console/api/v1/devices/device_00000000000001/remote-control", nil)
	recorder := httptest.NewRecorder()

	(&consoleHandler{}).writeRemote(recorder, request, http.StatusAccepted, nil, context.DeadlineExceeded)

	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status=%d want=%d", recorder.Code, http.StatusGatewayTimeout)
	}
	var envelope struct {
		Error struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "REMOTE_CONTROL_TIMEOUT" || !envelope.Error.Retryable {
		t.Fatalf("error=%#v", envelope.Error)
	}
}
