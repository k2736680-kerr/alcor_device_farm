package hostcommand

import "testing"

func TestReportedSessionFenceEndpointRequiresMacOSAndHTTPSOffHost(t *testing.T) {
	macOS := "macos"
	tests := []struct {
		name     string
		value    any
		hostOS   *string
		accepted bool
	}{
		{name: "loopback HTTP", value: "http://127.0.0.1:4810/", hostOS: &macOS, accepted: true},
		{name: "remote HTTPS", value: "https://ios-host.internal.example/fence/", hostOS: &macOS, accepted: true},
		{name: "remote HTTP", value: "http://192.0.2.10:4810", hostOS: &macOS},
		{name: "credentials", value: "https://user:secret@ios-host.internal.example", hostOS: &macOS},
		{name: "query", value: "https://ios-host.internal.example?token=secret", hostOS: &macOS},
		{name: "non macOS", value: "https://ios-host.internal.example", hostOS: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := reportedSessionFenceEndpoint(map[string]any{"session_fence_endpoint": test.value}, test.hostOS)
			if test.accepted {
				if err != nil || value == nil || (*value)[len(*value)-1:] == "/" {
					t.Fatalf("endpoint=%v error=%v", value, err)
				}
			} else if err == nil {
				t.Fatalf("endpoint must be rejected: %v", value)
			}
		})
	}
}
