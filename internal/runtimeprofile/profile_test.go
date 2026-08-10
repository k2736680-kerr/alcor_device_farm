package runtimeprofile

import "testing"

func TestParseSupportsLegacyImageResourcesAndCanonicalOverrides(t *testing.T) {
	profile, err := Parse(map[string]any{"cpu": 2.0, "memory_mb": 4096.0, "guest_memory_mb": 3072.0, "graphics": "software"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.ContainerCPUCores != 2 || profile.ContainerMemoryMB != 4096 || profile.GuestMemoryMB != 3072 || profile.Graphics != GraphicsSoftware {
		t.Fatalf("profile=%+v", profile)
	}
	canonical, err := Parse(profile.Map())
	if err != nil || canonical != profile {
		t.Fatalf("round trip=%+v error=%v", canonical, err)
	}
}

func TestParseRejectsGuestMemoryThatLeavesNoContainerHeadroom(t *testing.T) {
	_, err := Parse(map[string]any{"container_memory_mb": 4096, "guest_memory_mb": 4096})
	if err == nil {
		t.Fatal("expected invalid profile")
	}
}

func TestLegacyContainerLimitsDeriveGuestHeadroom(t *testing.T) {
	profile, err := Parse(map[string]any{"cpu": 2, "memory_mb": 4096})
	if err != nil {
		t.Fatal(err)
	}
	if profile.GuestCPUCores != 2 || profile.GuestMemoryMB != 3072 {
		t.Fatalf("profile=%+v", profile)
	}
}
