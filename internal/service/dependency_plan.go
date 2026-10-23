package service

import (
	"fmt"

	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"github.com/ddoyle2017/warden/internal/domain/metadata"
)

var ErrInvalidDependency = metadata.ErrInvalidDependency

type dependency struct {
	namespace, name string
}

func planDependencies(release thunderstore.Release) ([]dependency, error) {
	plan := make([]dependency, 0, len(release.Dependencies))
	for _, token := range release.Dependencies {
		dep, err := metadata.ParseDependency(token)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", release.Namespace, release.Name, err)
		}
		plan = append(plan, dependency{namespace: dep.Namespace, name: dep.Name})
	}
	return plan, nil
}
