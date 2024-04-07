package api

import "errors"

var (
	ErrHTTPClient = errors.New("HTTP client failed to complete request")
	ErrByteIO     = errors.New("failed to stream HTTP response body into an byte array")
	ErrJSONParse  = errors.New("failed to deserialize JSON into struct")
)
