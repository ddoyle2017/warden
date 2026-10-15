package service

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

const (
	yes     = "Y"
	no      = "n"
	yesOrNo = "[" + yes + "/" + no + "]"

	yesLong     = "YES I AM"
	noLong      = "no"
	yesOrNoLong = "[" + yesLong + "/" + noLong + "]"
)

var (
	ErrMaxAttempts       = errors.New("reached max confirmation attempts")
	ErrConfirmationInput = errors.New("unable to read confirmation")
)

func confirm(in *bufio.Scanner, affirmative, negative string) (bool, error) {
	for tries := 0; tries < 2; tries++ {
		scanned := in.Scan()
		// A reader can return an answer and an error together; do not approve a failed read.
		if err := in.Err(); err != nil {
			return false, fmt.Errorf("%w: %w", ErrConfirmationInput, err)
		}
		if !scanned {
			return false, fmt.Errorf("%w: %w", ErrConfirmationInput, io.EOF)
		}
		switch in.Text() {
		case affirmative:
			return true, nil
		case negative:
			return false, nil
		}
	}
	return false, ErrMaxAttempts
}
