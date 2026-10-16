package service

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

var ErrInvalidReleaseVersion = errors.New("invalid release version")

func hasNewerRelease(installed, latest string) (bool, error) {
	for _, version := range []struct{ source, number string }{
		{"installed", installed},
		{"latest", latest},
	} {
		if err := validateReleaseVersion(version.source, version.number); err != nil {
			return false, err
		}
	}
	return semver.Compare("v"+latest, "v"+installed) > 0, nil
}

func validateReleaseVersion(source, number string) error {
	core, _, _ := strings.Cut(number, "-")
	core, _, _ = strings.Cut(core, "+")
	// Thunderstore uses all three components, not Go's vMAJOR[.MINOR] shorthand.
	if strings.Count(core, ".") != 2 || !semver.IsValid("v"+number) {
		return fmt.Errorf("%w: %s version %q must be MAJOR.MINOR.PATCH with optional SemVer prerelease/build metadata",
			ErrInvalidReleaseVersion, source, number)
	}
	return nil
}
