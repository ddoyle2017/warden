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
		core, _, _ := strings.Cut(version.number, "-")
		core, _, _ = strings.Cut(core, "+")
		// Thunderstore uses all three components, not Go's vMAJOR[.MINOR] shorthand.
		if strings.Count(core, ".") != 2 || !semver.IsValid("v"+version.number) {
			return false, fmt.Errorf("%w: %s version %q must be MAJOR.MINOR.PATCH with optional SemVer prerelease/build metadata",
				ErrInvalidReleaseVersion, version.source, version.number)
		}
	}
	return semver.Compare("v"+latest, "v"+installed) > 0, nil
}
