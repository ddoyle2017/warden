package metadata

import (
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidDependency = errors.New("invalid dependency")

// Dependency retains the complete declared version, not a selected release.
type Dependency struct {
	Namespace string
	Name      string
	Version   string
}

func ParseDependency(token string) (Dependency, error) {
	parts := strings.SplitN(token, "-", 3)
	if len(parts) != 3 || !ValidPackageComponent(parts[0]) || !ValidPackageComponent(parts[1]) {
		return Dependency{}, fmt.Errorf("%w: dependency %q must be namespace-name-version; namespace and name must be nonempty ASCII letters, digits, or underscores",
			ErrInvalidDependency, token)
	}
	if err := ValidateReleaseVersion("dependency", parts[2]); err != nil {
		return Dependency{}, fmt.Errorf("%w: dependency %q: %w", ErrInvalidDependency, token, err)
	}
	return Dependency{Namespace: parts[0], Name: parts[1], Version: parts[2]}, nil
}

func ValidPackageComponent(component string) bool {
	if component == "" {
		return false
	}
	for _, c := range component {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}
