package mock

type ModRemoval struct {
	CommitFunc   func() error
	RollbackFunc func() error
}

func (r *ModRemoval) Commit() error {
	return r.CommitFunc()
}

func (r *ModRemoval) Rollback() error {
	return r.RollbackFunc()
}
