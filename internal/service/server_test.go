package service_test

import (
	"errors"
	"strings"
	"testing"
	"warden/internal/service"
)

func TestStart_Happy(t *testing.T) {
	tests := map[string]struct {
		config   string
		expected string
	}{
		"successfully starts vanilla game server": {
			config:   "vanilla",
			expected: "Starting Vanilla Server",
		},
		"successfully starts modded game server": {
			config:   "modded",
			expected: "Starting Modded Server",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ss := service.NewServerService("../test/data")

			output, err := ss.Start(tt.config)
			if err != nil {
				t.Errorf("unexpected error, received: %+v", err)
			}
			if strings.TrimSpace(output) != tt.expected {
				t.Errorf("expected output: %s, received: %s", tt.expected, output)
			}
		})
	}
}

func TestStart_Sad(t *testing.T) {
	ss := service.NewServerService("TEST-VALUE")
	_, err := ss.Start("niaudbiwabdiu dd")
	if !errors.Is(err, service.ErrInvalidGameType) {
		t.Errorf("expected error: %+v, received: %+v", service.ErrInvalidGameType, err)
	}
}

func TestIsValidGameType_Happy(t *testing.T) {
	tests := map[string]struct {
		config string
	}{
		"return true if game config is vanilla": {
			config: "vanilla",
		},
		"return true if game config is modded": {
			config: "modded",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ss := service.NewServerService("some-test-value")
			if !ss.IsValidGameType(tt.config) {
				t.Error("expected true, got false")
			}
		})
	}
}

func TestIsValidGameType_Sad(t *testing.T) {
	ss := service.NewServerService("some-test-value")

	if ss.IsValidGameType("RANDOM TEST VALUE") {
		t.Error("expected false, got true")
	}
}
