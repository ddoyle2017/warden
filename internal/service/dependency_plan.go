package service

import (
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"strings"
)

var ErrInvalidDependency = errors.New("invalid dependency")

type dependency struct {
	namespace, name string
}

func planDependencies(release thunderstore.Release) ([]dependency, error) {
	plan := make([]dependency, 0, len(release.Dependencies))
	for _, token := range release.Dependencies {
		parts := strings.SplitN(token, "-", 3)
		if len(parts) != 3 || !validDependencyComponent(parts[0]) || !validDependencyComponent(parts[1]) {
			return nil, fmt.Errorf("%w: %s/%s: dependency %q must be namespace-name-version; namespace and name must be nonempty ASCII letters, digits, or underscores",
				ErrInvalidDependency, release.Namespace, release.Name, token)
		}
		if err := validateReleaseVersion("dependency", parts[2]); err != nil {
			return nil, fmt.Errorf("%w: %s/%s: dependency %q: %w",
				ErrInvalidDependency, release.Namespace, release.Name, token, err)
		}
		plan = append(plan, dependency{namespace: parts[0], name: parts[1]})
	}
	return plan, nil
}

func validDependencyComponent(component string) bool {
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
