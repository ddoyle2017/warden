package thunderstore_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
	"warden/api"
	"warden/api/thunderstore"
	"warden/test/mock"
)

func TestGetPackage_Happy(t *testing.T) {
	namespace, name := "Azumatt", "Sleepover"
	expected := thunderstore.Package{
		Namespace: namespace,
		Name:      name,
	}

	body, err := mock.ResponseBodyToReader(expected)
	if err != nil {
		t.Errorf("failed to mock JSON response, received error: %v", err)
	}
	client := mock.HTTPClient{
		GetFunc: func(_ string) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       body,
			}, nil
		},
	}

	ts := thunderstore.New(&client)

	result, err := ts.GetPackage(namespace, name)
	if err != nil {
		t.Errorf("expected a nil error, got: %v", err)
	}
	if !result.Equals(&expected) {
		t.Errorf("expected Package: %+v, received: %+v", expected, result)
	}

}

func TestGetPackage_Sad(t *testing.T) {
	json := `{"name":"Test Name""full_name":"test full name,"owner"{"login": "octocat"}}}}}}}}}}}}}`
	invalidJSON := io.NopCloser(bytes.NewReader([]byte(json)))

	errorResponse, err := mock.ResponseBodyToReader(thunderstore.ErrorResponse{})
	if err != nil {
		t.Errorf("unexpected error during test set-up, err: %+v", err)
	}

	tests := map[string]struct {
		client      api.HTTPClient
		expectedErr error
	}{
		"HTTP client fails to send request": {
			client: &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{}, http.ErrBodyNotAllowed
				},
			},
			expectedErr: api.ErrHTTPClient,
		},
		"Thunderstore client fails to parse JSON into response struct": {
			client: &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       invalidJSON,
					}, nil
				},
			},
			expectedErr: api.ErrJSONParse,
		},
		"Thunderstore API returns a 404 Not Found": {
			client: &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Body:       errorResponse,
					}, nil
				},
			},
			expectedErr: thunderstore.ErrPackageNotFound,
		},
		"Thunderstore API returns a non-2xx response": {
			client: &mock.HTTPClient{
				GetFunc: func(_ string) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusInternalServerError,
						Body:       errorResponse,
					}, nil
				},
			},
			expectedErr: thunderstore.ErrThunderstoreAPI,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ts := thunderstore.New(test.client)

			result, err := ts.GetPackage("Azumatt", "Sleepover")
			if !errors.Is(err, test.expectedErr) {
				t.Errorf("expected error: %+v, got: %+v", test.expectedErr, err)
			}
			// Verify an empty package is returned for error cases
			if !result.Equals(&thunderstore.Package{}) {
				t.Errorf("expected Package: %v, received: %v", thunderstore.Package{}, result)
			}
		})
	}
}
