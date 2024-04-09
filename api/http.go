package api

import (
	"errors"
	"net/http"
)

var (
	ErrHTTPClient = errors.New("HTTP client failed to complete request")
	ErrByteIO     = errors.New("failed to stream HTTP response body into an byte array")
	ErrJSONParse  = errors.New("failed to deserialize JSON into struct")
)

// HTTPClient exposes an interface for basic HTTP operations Warden needs. Its fulfilled by Go's stdlib
// http.Client struct and mock.HTTPClient
type HTTPClient interface {
	Get(url string) (resp *http.Response, err error)
}
