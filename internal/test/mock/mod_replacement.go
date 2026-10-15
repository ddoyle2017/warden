package mock

type ModReplacement struct {
	PathFunc         func() string
	RecoveryPathFunc func() string
	CommitFunc       func() error
}

func (r *ModReplacement) Path() string {
	return r.PathFunc()
}

func (r *ModReplacement) RecoveryPath() string {
	return r.RecoveryPathFunc()
}

func (r *ModReplacement) Commit() error {
	return r.CommitFunc()
}
