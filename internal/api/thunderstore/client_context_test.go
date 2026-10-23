package thunderstore_test

import (
	"context"
	"errors"
	"github.com/ddoyle2017/warden/internal/api"
	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMetadataRequestContext(t *testing.T) {
	for operation, request := range metadataRequests() {
		t.Run("given a caller context, when fetching "+operation+" metadata, then retain its values and deadline", func(t *testing.T) {
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, "request identity")
			deadline := time.Now().Add(time.Hour)
			ctx, cancel := context.WithDeadline(ctx, deadline)
			t.Cleanup(cancel)
			calls, closes := 0, 0
			client := thunderstore.New(&mock.HTTPClient{DoFunc: func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Context() != ctx || req.Context().Value(contextKey{}) != "request identity" {
					t.Error("request lost the caller's context")
				}
				if got, ok := req.Context().Deadline(); !ok || !got.Equal(deadline) {
					t.Errorf("deadline = %v, present = %v, want %v", got, ok, deadline)
				}
				if req.Method != http.MethodGet || req.URL.String() != request.endpoint {
					t.Fatalf("unexpected request %s %s", req.Method, req.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: &metadataResponseBody{
					Reader: strings.NewReader(request.body), close: func() error { closes++; return nil },
				}}, nil
			}})
			result, err := request.fetch(ctx, client)
			if err != nil || !reflect.DeepEqual(result, request.want) {
				t.Errorf("result = %+v, error = %v, want %+v", result, err, request.want)
			}
			if calls != 1 || closes != 1 {
				t.Errorf("requests/closes = %d/%d, want 1/1", calls, closes)
			}
		})
	}
}

func TestMetadataRejectedContext(t *testing.T) {
	cause := errors.New("caller stopped the catalog lookup")
	for operation, request := range metadataRequests() {
		for _, scenario := range []string{"nil context", "cancellation", "deadline"} {
			t.Run("given "+scenario+", when fetching "+operation+" metadata, then reject before transport", func(t *testing.T) {
				var ctx context.Context
				wantErrs := []error{api.ErrHTTPClient}
				switch scenario {
				case "cancellation":
					var cancel context.CancelCauseFunc
					ctx, cancel = context.WithCancelCause(context.Background())
					cancel(cause)
					wantErrs = append(wantErrs, context.Canceled, cause)
				case "deadline":
					var cancel context.CancelFunc
					ctx, cancel = context.WithDeadlineCause(context.Background(), time.Unix(1, 0), cause)
					t.Cleanup(cancel)
					wantErrs = append(wantErrs, context.DeadlineExceeded, cause)
				}
				client := thunderstore.New(&mock.HTTPClient{DoFunc: func(*http.Request) (*http.Response, error) {
					t.Fatal("invalid or stopped context reached the transport")
					return nil, nil
				}})
				result, err := request.fetch(ctx, client)
				for _, wantErr := range wantErrs {
					if !errors.Is(err, wantErr) {
						t.Errorf("error = %v, want cause %v", err, wantErr)
					}
				}
				if err == nil || !strings.Contains(err.Error(), "GET "+request.endpoint) {
					t.Errorf("error lost request identity: %v", err)
				}
				if !reflect.DeepEqual(result, request.zero) {
					t.Errorf("failed request returned metadata: %+v", result)
				}
			})
		}
	}
}

func TestMetadataInFlightCancellation(t *testing.T) {
	cause := errors.New("catalog caller disconnected")
	for operation, request := range metadataRequests() {
		for _, phase := range []string{"transport", "body read", "successful response racing cancellation"} {
			t.Run("given a pending "+phase+", when cancelling "+operation+" metadata, then retain the cancellation cause", func(t *testing.T) {
				ctx, cancel := context.WithCancelCause(context.Background())
				t.Cleanup(func() { cancel(cause) })
				started := make(chan struct{})
				calls, closes := 0, 0
				client := thunderstore.New(&mock.HTTPClient{DoFunc: func(req *http.Request) (*http.Response, error) {
					calls++
					if req.Context() != ctx || req.Method != http.MethodGet || req.URL.String() != request.endpoint {
						return nil, errors.New("request did not preserve the caller context and metadata endpoint")
					}
					if phase != "body read" {
						close(started)
						<-req.Context().Done()
						if phase == "transport" {
							return nil, req.Context().Err()
						}
					}
					var reader io.Reader = strings.NewReader(request.body)
					if phase == "body read" {
						reader = metadataReadFunc(func([]byte) (int, error) {
							close(started)
							<-req.Context().Done()
							return 0, req.Context().Err()
						})
					}
					return &http.Response{StatusCode: http.StatusOK, Body: &metadataResponseBody{
						Reader: reader, close: func() error { closes++; return nil },
					}}, nil
				}})
				type outcome struct {
					value any
					err   error
				}
				finished := make(chan outcome, 1)
				go func() {
					value, err := request.fetch(ctx, client)
					finished <- outcome{value: value, err: err}
				}()
				select {
				case <-started:
				case early := <-finished:
					t.Fatalf("request completed before cancellation: %+v, %v", early.value, early.err)
				}
				cancel(cause)
				result := <-finished
				classification, wantCloses := api.ErrHTTPClient, 1
				if phase == "transport" {
					wantCloses = 0
				}
				if phase == "body read" {
					classification = api.ErrByteIO
				}
				for _, wantErr := range []error{classification, context.Canceled, cause} {
					if !errors.Is(result.err, wantErr) {
						t.Errorf("error = %v, want cause %v", result.err, wantErr)
					}
				}
				if result.err == nil || !strings.Contains(result.err.Error(), "GET "+request.endpoint) {
					t.Errorf("error lost request identity: %v", result.err)
				}
				if !reflect.DeepEqual(result.value, request.zero) || calls != 1 || closes != wantCloses {
					t.Errorf("result = %+v, requests/closes = %d/%d, want empty metadata and 1/%d", result.value, calls, closes, wantCloses)
				}
			})
		}
	}
}

func TestMetadataEscapedURLs(t *testing.T) {
	const namespace, name, version = "Owner/Team ?#%", "Package/Name ?#%", "1.0.0-rc.1+linux/x64?meta#%"
	const packageEndpoint = "https://thunderstore.io/api/experimental/package/Owner%2FTeam%20%3F%23%25/Package%2FName%20%3F%23%25"
	for operation, tt := range map[string]struct {
		endpoint string
		want     any
		fetch    func(*thunderstore.Client) (any, error)
	}{
		"package": {
			endpoint: packageEndpoint,
			want:     thunderstore.Package{Namespace: namespace, Name: name},
			fetch: func(client *thunderstore.Client) (any, error) {
				return client.GetPackageContext(context.Background(), namespace, name)
			},
		},
		"release": {
			endpoint: packageEndpoint + "/1.0.0-rc.1+linux%2Fx64%3Fmeta%23%25/",
			want: thunderstore.Release{
				Namespace: namespace, Name: name, VersionNumber: version,
				FullName: namespace + "-" + name + "-" + version, Dependencies: []string{},
			},
			fetch: func(client *thunderstore.Client) (any, error) {
				return client.GetReleaseContext(context.Background(), namespace, name, version)
			},
		},
	} {
		t.Run("given reserved characters, when fetching "+operation+" metadata, then escape each path component", func(t *testing.T) {
			calls, closes := 0, 0
			client := thunderstore.New(&mock.HTTPClient{DoFunc: func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodGet || req.URL.String() != tt.endpoint || req.URL.RawQuery != "" || req.URL.Fragment != "" {
					t.Fatalf("unsafe metadata request: %s %s", req.Method, req.URL)
				}
				body, err := mock.ResponseBodyToReader(tt.want)
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: &metadataResponseBody{
					Reader: body, close: func() error { closes++; return body.Close() },
				}}, nil
			}})
			result, err := tt.fetch(client)
			if err != nil || !reflect.DeepEqual(result, tt.want) {
				t.Errorf("result = %+v, error = %v, want %+v", result, err, tt.want)
			}
			if calls != 1 || closes != 1 {
				t.Errorf("requests/closes = %d/%d, want 1/1", calls, closes)
			}
		})
	}
}

func TestMetadataSilence(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "metadata-output")
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr := os.Stdout, os.Stderr
	t.Cleanup(func() {
		os.Stdout, os.Stderr = stdout, stderr
		if err := output.Close(); err != nil {
			t.Error(err)
		}
	})
	os.Stdout, os.Stderr = output, output
	for operation, request := range metadataRequests() {
		t.Run("given malformed JSON, when fetching "+operation+" metadata, then return the error without terminal output", func(t *testing.T) {
			client := thunderstore.New(&mock.HTTPClient{DoFunc: func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.String() != request.endpoint {
					t.Fatalf("unexpected request %s %s", req.Method, req.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{"))}, nil
			}})
			for _, fetch := range []func(*thunderstore.Client) (any, error){
				request.legacy,
				func(client *thunderstore.Client) (any, error) { return request.fetch(context.Background(), client) },
			} {
				result, err := fetch(client)
				if !errors.Is(err, api.ErrJSONParse) || !reflect.DeepEqual(result, request.zero) {
					t.Errorf("result = %+v, error = %v, want empty metadata and a JSON error", result, err)
				}
			}
		})
	}
	os.Stdout, os.Stderr = stdout, stderr
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Errorf("metadata fetches wrote to stdout or stderr: %q", data)
	}
}

type metadataReadFunc func([]byte) (int, error)

func (f metadataReadFunc) Read(p []byte) (int, error) { return f(p) }
