package mock

import "warden/internal/domain/framework"

type FrameworksRepo struct {
	GetFrameworkFunc    func(name string) (framework.Framework, error)
	InsertFrameworkFunc func(f framework.Framework) error
	UpdateFrameworkFunc func(f framework.Framework) error
	DeleteFrameworkFunc func(name string) error
}

func (r *FrameworksRepo) GetFramework(name string) (framework.Framework, error) {
	return r.GetFrameworkFunc(name)
}

func (r *FrameworksRepo) InsertFramework(f framework.Framework) error {
	return r.InsertFrameworkFunc(f)
}

func (r *FrameworksRepo) UpdateFramework(f framework.Framework) error {
	return r.UpdateFrameworkFunc(f)
}

func (r *FrameworksRepo) DeleteFramework(name string) error {
	return r.DeleteFrameworkFunc(name)
}
