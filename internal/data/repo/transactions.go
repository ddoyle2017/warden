package repo

import (
	"database/sql"
	"errors"
	"fmt"
)

func executeWrite(db Database, query string, executionErr error, args ...any) (err error) {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTransactionFailed, err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("rollback transaction: %w", rollbackErr))
		}
	}()

	statement, err := tx.Prepare(query)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidStatement, err)
	}
	if _, execErr := statement.Exec(args...); execErr != nil {
		err = fmt.Errorf("%w: %w", executionErr, execErr)
	}
	// Close before committing so a close failure can still abort the write.
	if closeErr := statement.Close(); closeErr != nil {
		err = errors.Join(err, fmt.Errorf("close statement: %w", closeErr))
	}
	if err != nil {
		return err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("commit transaction: %w", commitErr)
	}
	return nil
}
