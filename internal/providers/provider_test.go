package providers

import "testing"

func TestValidRuntimeImageReference(t *testing.T) {
	tests := map[string]bool{
		"registry.example/alcor/android-emulator:api36":                                                                   true,
		"registry.example:5000/alcor/android-emulator:api36-2026.08":                                                      true,
		"registry.example/alcor/android-emulator@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef": true,
		"": false,
		"registry.example/alcor/android-emulator":                                                                         false,
		"registry.example/alcor/android-emulator:":                                                                        false,
		"registry.example/alcor/android-emulator:latest":                                                                  false,
		"https://registry.example/alcor/android-emulator:api36":                                                           false,
		"registry.example/alcor/android-emulator@sha256:abc":                                                              false,
		"registry.example/alcor/android-emulator@sha256:0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF": false,
	}
	for value, expected := range tests {
		if actual := ValidRuntimeImageReference(value); actual != expected {
			t.Errorf("ValidRuntimeImageReference(%q)=%t, want %t", value, actual, expected)
		}
	}
}

func TestPlatformNeutralHealthRequiresCoreProbesButAcceptsUnsupportedRemoteControl(t *testing.T) {
	health := Health{Platform: PlatformIOS, Components: map[string]ProbeStatus{
		ProbeTransport: ProbePassed, ProbeOSReady: ProbePassed, ProbeAutomation: ProbePassed,
		ProbeRouter: ProbePassed, ProbeRemoteControl: ProbeUnsupported,
	}}
	if !health.Ready() {
		t.Fatal("healthy iOS component probes were not ready")
	}
	health.Components[ProbeAutomation] = ProbeFailed
	if health.Ready() {
		t.Fatal("failed automation probe was accepted as ready")
	}
	delete(health.Components, ProbeAutomation)
	if health.Ready() {
		t.Fatal("missing required automation probe was accepted as ready")
	}
}
