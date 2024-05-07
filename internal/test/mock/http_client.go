package mock

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

type HTTPClient struct {
	GetFunc func(url string) (resp *http.Response, err error)
}

func (hc *HTTPClient) Get(url string) (*http.Response, error) {
	return hc.GetFunc(url)
}

// ResponseBodyToReader() is a helper function for serializing a struct into JSON, then into
// an io.ReadCloser. This is helpful for mocking HTTP responses with the HTTPClient mock because
// io.ReadCloser is how Go's HTTP library represents response body data from HTTP responses.
func ResponseBodyToReader(body any) (io.ReadCloser, error) {
	json, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader([]byte(json))), nil
}
