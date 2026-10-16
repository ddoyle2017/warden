package service_test

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"warden/internal/data/file"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/mock"
)

func TestModRemoval_ReportsFinalizationErrors(t *testing.T) {
	tests := map[string]struct {
		all      bool
		rollback bool
	}{
		"given one committed removal, when cleanup fails, then report completion without rollback":  {},
		"given all removals committed, when cleanup fails, then report completion without rollback": {all: true},
		"given one record deletion failure, when rollback fails, then report both causes":           {rollback: true},
		"given all record deletions fail, when rollback fails, then report both causes":             {all: true, rollback: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			recordFailure := errors.New("record deletion outcome uncertain")
			finalizationFailure := errors.New("finalization failed")
			var events []string
			removal := &mock.ModRemoval{
				CommitFunc: func() error {
					events = append(events, "commit")
					return &file.ModRemovalCleanupError{Err: finalizationFailure}
				},
				RollbackFunc: func() error {
					events = append(events, "rollback")
					return finalizationFailure
				},
			}
			stage := func() (file.ModRemoval, error) {
				events = append(events, "stage")
				return removal, nil
			}
			deleteRecord := func() error {
				events = append(events, "records")
				if tt.rollback {
					return recordFailure
				}
				return nil
			}
			r := &mock.ModsRepo{
				GetModByIdentityFunc: func(string, string) (mod.Mod, error) {
					return mod.Mod{Namespace: "Owned", Name: "Installed", Version: "1.0.0"}, nil
				},
				DeleteModFunc:     func(string, string) error { return deleteRecord() },
				DeleteAllModsFunc: deleteRecord,
			}
			fm := &mock.Manager{
				StageModRemovalFunc:     func(string) (file.ModRemoval, error) { return stage() },
				StageAllModsRemovalFunc: stage,
			}
			answer := "Y"
			if tt.all {
				answer = "YES I AM"
			}
			ms := service.NewModService(r, fm, &mock.Thunderstore{}, strings.NewReader(answer))
			var err error
			if tt.all {
				err = ms.RemoveAllMods()
			} else {
				err = ms.RemoveMod("Owned", "Installed")
			}
			if !errors.Is(err, service.ErrUnableToRemoveMod) || !errors.Is(err, finalizationFailure) {
				t.Errorf("finalization cause was hidden: %v", err)
			}
			last := "commit"
			if tt.rollback {
				last = "rollback"
				if !errors.Is(err, recordFailure) || !strings.Contains(err.Error(), "verify database state") {
					t.Errorf("uncertain repository outcome was hidden: %v", err)
				}
			}
			if !slices.Equal(events, []string{"stage", "records", last}) {
				t.Errorf("unsafe removal ordering: %v", events)
			}
			var committed *file.ModRemovalCleanupError
			if errors.As(err, &committed) == tt.rollback {
				t.Errorf("incorrect committed-removal classification: %v", err)
			}
			if _, completed := err.(*service.ModRemovalCleanupError); completed == tt.rollback {
				t.Errorf("incorrect service-removal completion: %v", err)
			}
		})
	}
}

func TestModRemoval_DoesNotStageWithoutConfirmation(t *testing.T) {
	tests := map[string]struct {
		all     bool
		answer  string
		wantErr error
	}{
		"given a declined single removal, when returning, then perform no operations":      {answer: "n", wantErr: service.ErrCancelled},
		"given declined all-mod removal, when returning, then perform no operations":       {all: true, answer: "no", wantErr: service.ErrCancelled},
		"given exhausted single-removal input, when returning, then perform no operations": {wantErr: io.EOF},
		"given exhausted all-mod input, when returning, then perform no operations":        {all: true, wantErr: io.EOF},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			unexpected := func() {
				t.Fatal("removal accessed files or records without confirmation")
			}
			r := &mock.ModsRepo{
				GetModByIdentityFunc: func(string, string) (mod.Mod, error) {
					unexpected()
					return mod.Mod{}, nil
				},
				DeleteModFunc:     func(string, string) error { unexpected(); return nil },
				DeleteAllModsFunc: func() error { unexpected(); return nil },
			}
			fm := &mock.Manager{
				StageModRemovalFunc:     func(string) (file.ModRemoval, error) { unexpected(); return nil, nil },
				StageAllModsRemovalFunc: func() (file.ModRemoval, error) { unexpected(); return nil, nil },
			}
			ms := service.NewModService(r, fm, &mock.Thunderstore{}, strings.NewReader(tt.answer))
			var err error
			if tt.all {
				err = ms.RemoveAllMods()
			} else {
				err = ms.RemoveMod("Owned", "Installed")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("confirmation error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
