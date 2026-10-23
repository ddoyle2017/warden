package thunderstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/api"
	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

type metadataRequest struct {
	endpoint, body string
	want, zero     any
	fetch          func(context.Context, *thunderstore.Client) (any, error)
	legacy         func(*thunderstore.Client) (any, error)
}

func metadataRequests() map[string]metadataRequest {
	const releaseJSON = `{"namespace":"Owner","name":"Root","version_number":"1.0.0","full_name":"Owner-Root-1.0.0","dependencies":["Required-Dependency-2.0.0"],"description":"original release"}`
	release := thunderstore.Release{
		Namespace: "Owner", Name: "Root", VersionNumber: "1.0.0", FullName: "Owner-Root-1.0.0",
		Dependencies: []string{"Required-Dependency-2.0.0"}, Description: "original release",
	}
	return map[string]metadataRequest{
		"package": {
			endpoint: "https://thunderstore.io/api/experimental/package/Owner/Root",
			body:     `{"namespace":"Owner","name":"Root","latest":` + releaseJSON + `}`,
			want:     thunderstore.Package{Namespace: "Owner", Name: "Root", Latest: release},
			zero:     thunderstore.Package{},
			fetch: func(ctx context.Context, client *thunderstore.Client) (any, error) {
				return client.GetPackageContext(ctx, "Owner", "Root")
			},
			legacy: func(client *thunderstore.Client) (any, error) {
				return client.GetPackage("Owner", "Root")
			},
		},
		"release": {
			endpoint: "https://thunderstore.io/api/experimental/package/Owner/Root/1.0.0/",
			body:     releaseJSON,
			want:     release,
			zero:     thunderstore.Release{},
			fetch: func(ctx context.Context, client *thunderstore.Client) (any, error) {
				return client.GetReleaseContext(ctx, "Owner", "Root", "1.0.0")
			},
			legacy: func(client *thunderstore.Client) (any, error) {
				return client.GetRelease("Owner", "Root", "1.0.0")
			},
		},
	}
}

func TestMetadataResponses(t *testing.T) {
	readCause, closeCause := errors.New("body read interrupted"), errors.New("body close interrupted")
	for operation, request := range metadataRequests() {
		transportCause := &url.Error{Op: "Get", URL: request.endpoint, Err: io.ErrClosedPipe}
		for scenario, tt := range map[string]struct {
			body                         string
			status                       int
			transport, read, close       error
			wantErrs                     []error
			syntaxError, typeError       bool
			missingResponse, missingBody bool
		}{
			"a valid response": {
				body: request.body, status: http.StatusOK,
			},
			"a missing package or version": {
				body: `not JSON`, status: http.StatusNotFound, wantErrs: []error{thunderstore.ErrPackageNotFound},
			},
			"a server error": {
				body: `not JSON`, status: http.StatusInternalServerError, wantErrs: []error{thunderstore.ErrThunderstoreAPI},
			},
			"a rejected request": {
				body: `{}`, status: http.StatusForbidden, wantErrs: []error{thunderstore.ErrThunderstoreAPI},
			},
			"an unhandled redirect": {
				status: http.StatusFound, wantErrs: []error{thunderstore.ErrThunderstoreAPI},
			},
			"an empty successful status other than 200": {
				status: http.StatusNoContent, wantErrs: []error{thunderstore.ErrThunderstoreAPI},
			},
			"a transport failure": {
				transport: transportCause, wantErrs: []error{api.ErrHTTPClient, transportCause, io.ErrClosedPipe},
			},
			"a body read failure": {
				status: http.StatusOK, read: readCause, wantErrs: []error{api.ErrByteIO, readCause},
			},
			"a body close failure": {
				body: request.body, status: http.StatusOK, close: closeCause, wantErrs: []error{api.ErrByteIO, closeCause},
			},
			"body read and close failures": {
				status: http.StatusOK, read: readCause, close: closeCause, wantErrs: []error{api.ErrByteIO, readCause, closeCause},
			},
			"malformed JSON": {
				body: `{`, status: http.StatusOK, wantErrs: []error{api.ErrJSONParse}, syntaxError: true,
			},
			"a wrong JSON field type": {
				body: `{"name":42}`, status: http.StatusOK, wantErrs: []error{api.ErrJSONParse}, typeError: true,
			},
			"malformed JSON and a close failure": {
				body: `{`, status: http.StatusOK, close: closeCause, wantErrs: []error{api.ErrJSONParse, api.ErrByteIO, closeCause}, syntaxError: true,
			},
			"a missing resource and a close failure": {
				status: http.StatusNotFound, close: closeCause, wantErrs: []error{thunderstore.ErrPackageNotFound, api.ErrByteIO, closeCause},
			},
			"a server error with read and close failures": {
				status: http.StatusServiceUnavailable, read: readCause, close: closeCause,
				wantErrs: []error{thunderstore.ErrThunderstoreAPI, api.ErrByteIO, readCause, closeCause},
			},
			"a transport returning no response": {
				missingResponse: true, wantErrs: []error{api.ErrHTTPClient},
			},
			"a response with no body": {
				status: http.StatusOK, missingBody: true, wantErrs: []error{api.ErrHTTPClient},
			},
		} {
			for method, fetch := range map[string]func(*thunderstore.Client) (any, error){
				"context-aware": func(client *thunderstore.Client) (any, error) {
					return request.fetch(context.Background(), client)
				},
				"legacy": request.legacy,
			} {
				t.Run(operation+"/given "+scenario+", when using the "+method+" method, then retain results or classified causes", func(t *testing.T) {
					calls, closes := 0, 0
					client := thunderstore.New(&mock.HTTPClient{DoFunc: func(req *http.Request) (*http.Response, error) {
						calls++
						if req.Method != http.MethodGet || req.URL.String() != request.endpoint || req.Body != nil {
							t.Fatalf("unexpected metadata request: %s %s with body %v", req.Method, req.URL, req.Body)
						}
						if tt.transport != nil || tt.missingResponse {
							return nil, tt.transport
						}
						response := &http.Response{StatusCode: tt.status}
						if !tt.missingBody {
							var reader io.Reader = strings.NewReader(tt.body)
							if tt.read != nil {
								reader = iotest.ErrReader(tt.read)
							}
							response.Body = &metadataResponseBody{Reader: reader, close: func() error {
								closes++
								return tt.close
							}}
						}
						return response, nil
					}})
					result, err := fetch(client)
					want := request.zero
					if len(tt.wantErrs) == 0 {
						if err != nil {
							t.Fatalf("unexpected error: %v", err)
						}
						want = request.want
					} else {
						for _, cause := range tt.wantErrs {
							if !errors.Is(err, cause) {
								t.Errorf("error = %v, want cause %v", err, cause)
							}
						}
						if err == nil || !strings.Contains(err.Error(), "GET "+request.endpoint) {
							t.Errorf("error lost request identity: %v", err)
						}
						if tt.status != 0 && (err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", tt.status))) {
							t.Errorf("error lost HTTP status %d: %v", tt.status, err)
						}
						if tt.status != http.StatusOK && errors.Is(err, api.ErrJSONParse) {
							t.Errorf("failure status was treated as metadata: %v", err)
						}
					}
					if tt.transport != nil {
						var original *url.Error
						if !errors.As(err, &original) || original != transportCause {
							t.Errorf("transport error was not preserved for errors.As: %v", err)
						}
					}
					if tt.syntaxError {
						var original *json.SyntaxError
						if !errors.As(err, &original) || original.Offset == 0 {
							t.Errorf("JSON syntax error and offset were lost: %v", err)
						}
					}
					if tt.typeError {
						var original *json.UnmarshalTypeError
						if !errors.As(err, &original) || original.Field != "name" {
							t.Errorf("JSON type error and field were lost: %v", err)
						}
					}
					if !reflect.DeepEqual(result, want) {
						t.Errorf("result = %+v, want %+v", result, want)
					}
					wantCloses := 1
					if tt.transport != nil || tt.missingResponse || tt.missingBody {
						wantCloses = 0
					}
					if calls != 1 || closes != wantCloses {
						t.Errorf("requests/closes = %d/%d, want 1/%d", calls, closes, wantCloses)
					}
				})
			}
		}
	}
}

func TestGetPackageSparseMetadata(t *testing.T) {
	for scenario, tt := range map[string]struct {
		body string
		want thunderstore.Package
	}{
		"identity without release fields": {body: `{"namespace":"Owner","name":"Root"}`, want: thunderstore.Package{Namespace: "Owner", Name: "Root"}},
		"an empty object":                 {body: `{}`},
		"a null value":                    {body: `null`},
	} {
		t.Run("given "+scenario+", when fetching a package, then preserve legacy value decoding", func(t *testing.T) {
			calls, closes := 0, 0
			client := thunderstore.New(&mock.HTTPClient{DoFunc: func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodGet || req.URL.String() != "https://thunderstore.io/api/experimental/package/Owner/Root" {
					t.Fatalf("unexpected request %s %s", req.Method, req.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: &metadataResponseBody{
					Reader: strings.NewReader(tt.body), close: func() error { closes++; return nil },
				}}, nil
			}})
			for i := 0; i < 2; i++ {
				result, err := client.GetPackage("Owner", "Root")
				if err != nil || !reflect.DeepEqual(result, tt.want) {
					t.Errorf("result = %+v, error = %v, want %+v", result, err, tt.want)
				}
			}
			if calls != 2 || closes != 2 {
				t.Errorf("requests/closes = %d/%d, want 2/2", calls, closes)
			}
		})
	}
}

type metadataResponseBody struct {
	io.Reader
	close func() error
}

func (b *metadataResponseBody) Close() error { return b.close() }
