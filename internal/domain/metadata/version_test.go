package metadata_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ddoyle2017/warden/internal/domain/metadata"
)

func TestCompareReleaseVersions(t *testing.T) {
	for name, tc := range map[string]struct {
		installed, latest string
		want              int
	}{
		"a numeric minor upgrade":        {"1.9.0", "1.10.0", -1},
		"a numeric patch upgrade":        {"1.0.9", "1.0.10", -1},
		"a major downgrade":              {"2.0.0", "1.99.99", 1},
		"equal versions":                 {"1.2.3", "1.2.3", 0},
		"different build metadata":       {"1.2.3+first.001", "1.2.3+second.002", 0},
		"a numeric prerelease upgrade":   {"1.0.0-beta.9", "1.0.0-beta.10", -1},
		"a stable release":               {"1.0.0-rc.1", "1.0.0", -1},
		"a prerelease downgrade":         {"1.0.0", "1.0.0-rc.1", 1},
		"an alphabetic prerelease":       {"1.0.0-9", "1.0.0-alpha", -1},
		"a longer prerelease":            {"1.0.0-alpha", "1.0.0-alpha.1", -1},
		"hyphenated prerelease metadata": {"1.0.0-alpha-beta.9+linux-x64.001", "1.0.0-alpha-beta.10+linux-x64.002", -1},
		"components beyond uint64":       {"18446744073709551615.0.0", "18446744073709551616.0.0", -1},
	} {
		t.Run("given "+name+", when comparing, then order installed relative to latest", func(t *testing.T) {
			got, err := metadata.CompareReleaseVersions(tc.installed, tc.latest)
			if err != nil || got != tc.want {
				t.Fatalf("comparison = %d, %v; want %d", got, err, tc.want)
			}
			reverse, err := metadata.CompareReleaseVersions(tc.latest, tc.installed)
			if err != nil || reverse != -tc.want {
				t.Errorf("reversed comparison = %d, %v; want %d", reverse, err, -tc.want)
			}
		})
	}
}

func TestReleaseVersionValidation(t *testing.T) {
	for _, version := range []string{
		"", "broken", "1", "1.0", "v1.0.0", "1.0.0.0", "01.0.0", "1.01.0", "1.0.01",
		"1.0.0-01", "1.0.0-alpha..1", "1.0.0+", " 1.0.0", "1.0.0 ", "1.0.0-\u00e9",
	} {
		for _, source := range []string{"installed", "latest"} {
			t.Run("given "+source+" version "+version+", when comparing, then reject invalid SemVer", func(t *testing.T) {
				installed, latest := "1.0.0", "2.0.0"
				if source == "installed" {
					installed = version
				} else {
					latest = version
				}
				order, err := metadata.CompareReleaseVersions(installed, latest)
				if order != 0 || !errors.Is(err, metadata.ErrInvalidReleaseVersion) {
					t.Fatalf("invalid comparison = %d, %v", order, err)
				}
				if !strings.Contains(err.Error(), source+" version") || !strings.Contains(err.Error(), `"`+version+`"`) {
					t.Errorf("invalid version diagnostic lost its source or value: %v", err)
				}
			})
		}
	}
	t.Run("given two invalid versions, when comparing, then validate installed first", func(t *testing.T) {
		_, err := metadata.CompareReleaseVersions("bad-installed", "bad-latest")
		if !errors.Is(err, metadata.ErrInvalidReleaseVersion) || !strings.Contains(err.Error(), "installed version") ||
			strings.Contains(err.Error(), "bad-latest") {
			t.Errorf("first invalid version = %v", err)
		}
	})
}
