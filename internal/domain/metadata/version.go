package metadata

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

var ErrInvalidReleaseVersion = errors.New("invalid release version")

// CompareReleaseVersions returns -1, 0, or 1 when installed is older than, equal
// in precedence to, or newer than latest. It validates installed before latest.
// Build metadata does not affect precedence.
func CompareReleaseVersions(installed, latest string) (int, error) {
	for _, version := range []struct{ source, number string }{
		{"installed", installed},
		{"latest", latest},
	} {
		if err := ValidateReleaseVersion(version.source, version.number); err != nil {
			return 0, err
		}
	}
	return semver.Compare("v"+installed, "v"+latest), nil
}

func ValidateReleaseVersion(source, number string) error {
	core, _, _ := strings.Cut(number, "-")
	core, _, _ = strings.Cut(core, "+")
	// Thunderstore uses all three components, not Go's vMAJOR[.MINOR] shorthand.
	if strings.Count(core, ".") != 2 || !semver.IsValid("v"+number) {
		return fmt.Errorf("%w: %s version %q must be MAJOR.MINOR.PATCH with optional SemVer prerelease/build metadata",
			ErrInvalidReleaseVersion, source, number)
	}
	return nil
}
