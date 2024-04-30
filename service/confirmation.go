package service

import "errors"

const (
	yes     = "Y"
	no      = "n"
	yesOrNo = "[" + yes + "/" + no + "]"

	yesLong     = "YES I AM"
	noLong      = "no"
	yesOrNoLong = "[" + yesLong + "/" + noLong + "]"
)

var (
	ErrMaxAttempts = errors.New("reached max confirmation attempts")
)
