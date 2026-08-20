package baguette

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeAuthorizer struct{ err error }

func (authorizer *fakeAuthorizer) AuthorizeBaguette(context.Context, Ticket) error {
	return authorizer.err
}

func TestGatewayUsesReservationCookieAndExposesOnlyTargetSimulator(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/simulators.json":
			_, _ = writer.Write([]byte(`{"running":[{"udid":"` + testUDID + `","state":"Booted"},{"udid":"OTHER","state":"Booted"}],"available":[]}`))
		case "/simulators/" + testUDID:
			_, _ = writer.Write([]byte("Baguette 原生页面"))
		case "/simulators/" + testUDID + "/screenshot.png":
			_, _ = writer.Write([]byte("image"))
		case "/sim-native.js":
			_, _ = writer.Write([]byte("native asset"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer upstream.Close()
	client := newTestClient(t, upstream.URL, time.Now())
	authorizer := &fakeAuthorizer{}
	gateway, err := NewGateway(client, authorizer)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := client.EntryURL(testDevice, testUDID, testReservation, "admin")
	entryPath := strings.TrimPrefix(entry, "http://gateway.example.test:18081")
	entryResponse := httptest.NewRecorder()
	gateway.ServeHTTP(entryResponse, httptest.NewRequest(http.MethodGet, entryPath, nil))
	if entryResponse.Code != http.StatusFound || entryResponse.Header().Get("Location") != "/simulators/"+testUDID {
		t.Fatalf("入口状态=%d 响应头=%v", entryResponse.Code, entryResponse.Header())
	}
	cookies := entryResponse.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].MaxAge != 0 {
		t.Fatalf("会话 Cookie=%v", cookies)
	}

	request := func(method, path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		req.AddCookie(cookies[0])
		gateway.ServeHTTP(recorder, req)
		return recorder
	}
	if response := request(http.MethodGet, "/simulators/"+testUDID); response.Code != http.StatusOK || response.Body.String() != "Baguette 原生页面" {
		t.Fatalf("目标页面状态=%d 内容=%s", response.Code, response.Body.String())
	}
	if response := request(http.MethodGet, "/simulators.json"); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), testUDID) || strings.Contains(response.Body.String(), "OTHER") {
		t.Fatalf("过滤清单状态=%d 内容=%s", response.Code, response.Body.String())
	}
	plugins := request(http.MethodGet, "/plugins.json")
	if plugins.Code != http.StatusOK || plugins.Body.String() != `{"plugins":[]}` {
		t.Fatalf("插件清单状态=%d 内容=%s", plugins.Code, plugins.Body.String())
	}
	for _, path := range []string{"/simulators/OTHER", "/simulators/" + testUDID + "/boot", "/farm", "/plugins/example/commands/run"} {
		if response := request(http.MethodGet, path); response.Code != http.StatusForbidden {
			t.Fatalf("越权路径=%s 状态=%d 内容=%s", path, response.Code, response.Body.String())
		}
	}
	authorizer.err = errors.New("预约已释放")
	if response := request(http.MethodGet, "/sim-native.js"); response.Code != http.StatusUnauthorized {
		t.Fatalf("释放后状态=%d 内容=%s", response.Code, response.Body.String())
	}
}

func TestGatewayRejectsExpiredOrTamperedEntry(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer upstream.Close()
	now := time.Now()
	client := newTestClient(t, upstream.URL, now)
	gateway, _ := NewGateway(client, &fakeAuthorizer{})
	entry, _ := client.EntryURL(testDevice, testUDID, testReservation, "admin")
	token := strings.TrimPrefix(entry, "http://gateway.example.test:18081/entry/")
	client.now = func() time.Time { return now.Add(time.Minute) }
	for _, candidate := range []string{token, token + "x"} {
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/entry/"+candidate, nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("无效票据状态=%d", response.Code)
		}
	}
}
