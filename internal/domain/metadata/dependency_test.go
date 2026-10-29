package metadata_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ddoyle2017/warden/internal/domain/metadata"
)

func TestParseDependency(t *testing.T) {
	for _, tc := range []struct {
		token string
		want  metadata.Dependency
	}{
		{"Team_42-Dependency_9-1.2.3", metadata.Dependency{Namespace: "Team_42", Name: "Dependency_9", Version: "1.2.3"}},
		{"Team_42-Dependency_9-1.2.3-alpha-beta.10+linux-x64.001",
			metadata.Dependency{Namespace: "Team_42", Name: "Dependency_9", Version: "1.2.3-alpha-beta.10+linux-x64.001"}},
		{"denikson-BepInExPack_Valheim-5.4.2202",
			metadata.Dependency{Namespace: "denikson", Name: "BepInExPack_Valheim", Version: "5.4.2202"}},
	} {
		t.Run("given "+tc.token+", when parsing, then preserve every component", func(t *testing.T) {
			got, err := metadata.ParseDependency(tc.token)
			if err != nil || got != tc.want {
				t.Errorf("dependency = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		token          string
		invalidVersion bool
	}{
		{"", false}, {"Owner", false}, {"Owner-Name", false}, {"-Name-1.0.0", false},
		{"Owner--1.0.0", false}, {"Owner/Team-Name-1.0.0", false}, {"Owner-Name.Other-1.0.0", false},
		{"T\u00e9am-Name-1.0.0", false}, {"Owner-Name\u0663-1.0.0", false}, {" Owner-Name-1.0.0", false},
		{"Owner-Name-", true}, {"Owner-Name-v1.0.0", true}, {"Owner-Name-1.0", true},
		{"Owner-Name-1.0.0-01", true}, {"Owner-Name-1.0.0+", true},
		{"denikson-BepInExPack_Valheim-broken", true},
	} {
		t.Run("given invalid token "+tc.token+", when parsing, then retain dependency and version causes", func(t *testing.T) {
			got, err := metadata.ParseDependency(tc.token)
			if got != (metadata.Dependency{}) || !errors.Is(err, metadata.ErrInvalidDependency) ||
				errors.Is(err, metadata.ErrInvalidReleaseVersion) != tc.invalidVersion {
				t.Fatalf("dependency = %+v, %v", got, err)
			}
			if !strings.Contains(err.Error(), `"`+tc.token+`"`) {
				t.Errorf("error lost the declared token: %v", err)
			}
		})
	}
}
