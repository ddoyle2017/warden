package service

import "github.com/ddoyle2017/warden/internal/domain/metadata"

var ErrInvalidReleaseVersion = metadata.ErrInvalidReleaseVersion

func hasNewerRelease(installed, latest string) (bool, error) {
	order, err := metadata.CompareReleaseVersions(installed, latest)
	return order < 0, err
}

func validateReleaseVersion(source, number string) error {
	return metadata.ValidateReleaseVersion(source, number)
}
