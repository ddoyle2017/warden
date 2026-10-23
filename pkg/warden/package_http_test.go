package warden_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/ddoyle2017/warden/internal/api"
	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"github.com/ddoyle2017/warden/pkg/warden"
)

func TestGetPackageRemoteErrors(t *testing.T) {
	transportCause := &url.Error{Op: "Get", URL: catalogEndpoint, Err: io.ErrClosedPipe}
	readCause, closeCause := errors.New("body read failed"), errors.New("body close failed")
	for name, tc := range map[string]struct {
		body                        string
		status                      int
		transport, read, close      error
		missingResponse             bool
		want                        []error
		syntaxError, fieldTypeError bool
	}{
		"not found": {
			status: http.StatusNotFound, body: "not JSON",
			want: []error{warden.ErrPackageNotFound, thunderstore.ErrPackageNotFound},
		},
		"server failure": {
			status: http.StatusServiceUnavailable, body: "not JSON",
			want: []error{warden.ErrRemoteFailure, thunderstore.ErrThunderstoreAPI},
		},
		"forbidden": {
			status: http.StatusForbidden, body: "{}",
			want: []error{warden.ErrRemoteFailure, thunderstore.ErrThunderstoreAPI},
		},
		"unexpected success status": {
			status: http.StatusNoContent,
			want:   []error{warden.ErrRemoteFailure, thunderstore.ErrThunderstoreAPI},
		},
		"transport failure": {
			transport: transportCause, want: []error{warden.ErrRemoteFailure, api.ErrHTTPClient, transportCause, io.ErrClosedPipe},
		},
		"missing response": {
			missingResponse: true, want: []error{warden.ErrRemoteFailure, api.ErrHTTPClient},
		},
		"body read failure": {
			status: http.StatusOK, read: readCause, want: []error{warden.ErrRemoteFailure, api.ErrByteIO, readCause},
		},
		"body close failure after valid data": {
			status: http.StatusOK, body: catalogJSON, close: closeCause,
			want: []error{warden.ErrRemoteFailure, api.ErrByteIO, closeCause},
		},
		"body read and close failure": {
			status: http.StatusOK, read: readCause, close: closeCause,
			want: []error{warden.ErrRemoteFailure, api.ErrByteIO, readCause, closeCause},
		},
		"invalid JSON": {
			status: http.StatusOK, body: "{", syntaxError: true,
			want: []error{warden.ErrInvalidMetadata, api.ErrJSONParse},
		},
		"wrong JSON field type": {
			status: http.StatusOK, body: `{"name":42}`, fieldTypeError: true,
			want: []error{warden.ErrInvalidMetadata, api.ErrJSONParse},
		},
		"wrong dependency list type": {
			status: http.StatusOK, body: `{"latest":{"dependencies":"not a list"}}`,
			want: []error{warden.ErrInvalidMetadata, api.ErrJSONParse},
		},
		"wrong dependency element type": {
			status: http.StatusOK, body: `{"latest":{"dependencies":[42]}}`,
			want: []error{warden.ErrInvalidMetadata, api.ErrJSONParse},
		},
		"wrong deprecated flag type": {
			status: http.StatusOK, body: `{"is_deprecated":"false"}`,
			want: []error{warden.ErrInvalidMetadata, api.ErrJSONParse},
		},
		"invalid JSON and close failure": {
			status: http.StatusOK, body: "{", close: closeCause, syntaxError: true,
			want: []error{warden.ErrInvalidMetadata, warden.ErrRemoteFailure, api.ErrJSONParse, api.ErrByteIO, closeCause},
		},
		"not found and close failure": {
			status: http.StatusNotFound, close: closeCause,
			want: []error{warden.ErrPackageNotFound, warden.ErrRemoteFailure, thunderstore.ErrPackageNotFound, api.ErrByteIO, closeCause},
		},
		"server status and body failures": {
			status: http.StatusInternalServerError, read: readCause, close: closeCause,
			want: []error{warden.ErrRemoteFailure, thunderstore.ErrThunderstoreAPI, api.ErrByteIO, readCause, closeCause},
		},
	} {
		t.Run("given "+name+", when inspecting, then preserve classifications and causes without partial values", func(t *testing.T) {
			calls, closes := 0, 0
			reader := openCatalogReader(t, catalogClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				if tc.transport != nil || tc.missingResponse {
					return nil, tc.transport
				}
				var body io.Reader = strings.NewReader(tc.body)
				if tc.read != nil {
					body = iotest.ErrReader(tc.read)
				}
				return &http.Response{StatusCode: tc.status, Body: &catalogBody{
					Reader: body, close: func() error { closes++; return tc.close },
				}}, nil
			}))
			id := catalogPackage().ID
			got, err := reader.GetPackage(context.Background(), id)
			requirePackageError(t, got, err, id, tc.want...)
			if tc.syntaxError {
				var original *json.SyntaxError
				if !errors.As(err, &original) {
					t.Errorf("JSON syntax cause lost: %v", err)
				}
			}
			if tc.fieldTypeError {
				var original *json.UnmarshalTypeError
				if !errors.As(err, &original) || original.Field != "name" {
					t.Errorf("JSON type cause lost: %v", err)
				}
			}
			if tc.transport != nil {
				var original *url.Error
				if !errors.As(err, &original) || original.URL != catalogEndpoint {
					t.Errorf("HTTP cause lost: %v", err)
				}
			}
			if tc.status != 0 && tc.status != http.StatusOK && errors.Is(err, warden.ErrInvalidMetadata) {
				t.Errorf("error status was treated as successful metadata: %v", err)
			}
			if tc.status != 0 && !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", tc.status)) {
				t.Errorf("HTTP status lost: %v", err)
			}
			wantCloses := 1
			if tc.transport != nil || tc.missingResponse {
				wantCloses = 0
			}
			if calls != 1 || closes != wantCloses {
				t.Errorf("requests/closes = %d/%d; want 1/%d", calls, closes, wantCloses)
			}
		})
	}
}

func TestGetPackageContext(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "catalog caller")
	deadline := time.Now().Add(time.Hour)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	t.Cleanup(cancel)
	calls := 0
	reader := openCatalogReader(t, catalogClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Context() != ctx || req.Context().Value(contextKey{}) != "catalog caller" {
			t.Error("catalog request lost the caller's context")
		}
		if got, ok := req.Context().Deadline(); !ok || !got.Equal(deadline) {
			t.Errorf("request deadline = %v, %v; want %v", got, ok, deadline)
		}
		return catalogResponse(catalogJSON), nil
	}))
	got, err := reader.GetPackage(ctx, catalogPackage().ID)
	if err != nil || !reflect.DeepEqual(got, catalogPackage()) || calls != 1 {
		t.Fatalf("context lookup = %#v, %v; calls = %d", got, err, calls)
	}
}

func TestGetPackageRejectedContext(t *testing.T) {
	cause := errors.New("catalog caller stopped")
	for _, scenario := range []string{"nil context", "cancellation", "expired deadline"} {
		t.Run("given "+scenario+", when inspecting, then reject before HTTP with the original cause", func(t *testing.T) {
			var ctx context.Context
			want := []error{warden.ErrRemoteFailure, api.ErrHTTPClient}
			switch scenario {
			case "cancellation":
				var cancel context.CancelCauseFunc
				ctx, cancel = context.WithCancelCause(context.Background())
				cancel(cause)
				want = append(want, context.Canceled, cause)
			case "expired deadline":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadlineCause(context.Background(), time.Unix(1, 0), cause)
				t.Cleanup(cancel)
				want = append(want, context.DeadlineExceeded, cause)
			}
			reader := openCatalogReader(t, catalogClient(t, func(*http.Request) (*http.Response, error) {
				t.Fatal("stopped context reached HTTP")
				return nil, nil
			}))
			id := catalogPackage().ID
			got, err := reader.GetPackage(ctx, id)
			requirePackageError(t, got, err, id, want...)
		})
	}
}

func TestGetPackageInFlightCancellation(t *testing.T) {
	for _, phase := range []string{"transport", "body read", "successful body close"} {
		t.Run("given pending "+phase+", when cancelling, then retain the context cause and no package", func(t *testing.T) {
			cause := errors.New("catalog caller disconnected")
			ctx, cancel := context.WithCancelCause(context.Background())
			t.Cleanup(func() { cancel(cause) })
			started := make(chan struct{})
			calls, closes := 0, 0
			reader := openCatalogReader(t, catalogClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Context() != ctx {
					return nil, errors.New("request lost its context")
				}
				if phase == "transport" {
					close(started)
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				var body io.Reader = strings.NewReader(catalogJSON)
				if phase == "body read" {
					body = catalogReadFunc(func([]byte) (int, error) {
						close(started)
						<-req.Context().Done()
						return 0, req.Context().Err()
					})
				}
				return &http.Response{StatusCode: http.StatusOK, Body: &catalogBody{
					Reader: body, close: func() error {
						closes++
						if phase == "successful body close" {
							close(started)
							<-req.Context().Done()
						}
						return nil
					},
				}}, nil
			}))
			type outcome struct {
				pkg warden.Package
				err error
			}
			finished := make(chan outcome, 1)
			id := catalogPackage().ID
			go func() {
				pkg, err := reader.GetPackage(ctx, id)
				finished <- outcome{pkg, err}
			}()
			select {
			case <-started:
			case early := <-finished:
				t.Fatalf("lookup finished before cancellation: %+v, %v", early.pkg, early.err)
			}
			cancel(cause)
			result := <-finished
			requirePackageError(t, result.pkg, result.err, id, warden.ErrRemoteFailure, context.Canceled, cause)
			wantCloses := 1
			if phase == "transport" {
				wantCloses = 0
			}
			if calls != 1 || closes != wantCloses {
				t.Errorf("requests/closes = %d/%d; want 1/%d", calls, closes, wantCloses)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGetPackageDefaultClient(t *testing.T) {
	calls := 0
	var start time.Time
	transport := &catalogTransport{roundTrip: func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodGet || req.URL.String() != catalogEndpoint {
			t.Errorf("unexpected default-client request: %s %s", req.Method, req.URL)
		}
		deadline, ok := req.Context().Deadline()
		if !ok || deadline.Before(start.Add(30*time.Second)) || deadline.After(time.Now().Add(30*time.Second)) {
			t.Errorf("default request deadline = %v, present = %v; want a 30-second client timeout", deadline, ok)
		}
		return catalogResponse(catalogJSON), nil
	}}
	original := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original })
	reader := openCatalogReader(t, nil)
	if calls != 0 {
		t.Fatal("Open contacted the default HTTP transport")
	}
	for i := 0; i < 2; i++ {
		start = time.Now()
		got, err := reader.GetPackage(context.Background(), catalogPackage().ID)
		if err != nil || !reflect.DeepEqual(got, catalogPackage()) {
			t.Fatalf("default-client lookup = %#v, %v", got, err)
		}
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || transport.idleCloses != 0 {
		t.Errorf("default transport requests/idle closes = %d/%d; want 2/0", calls, transport.idleCloses)
	}
}

func TestGetPackageSuppliedClientOwnership(t *testing.T) {
	calls, redirectCalls := 0, 0
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	first := &catalogTransport{roundTrip: func(*http.Request) (*http.Response, error) {
		calls++
		return catalogResponse(catalogJSON), nil
	}}
	client := &http.Client{
		Timeout: time.Minute, Transport: first, Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { redirectCalls++; return http.ErrUseLastResponse },
	}
	reader := openCatalogReader(t, client)
	if calls != 0 || client.Timeout != time.Minute || client.Transport != first || client.Jar != jar {
		t.Fatal("Open used or modified the supplied client")
	}
	got, err := reader.GetPackage(context.Background(), catalogPackage().ID)
	if err != nil || !reflect.DeepEqual(got, catalogPackage()) {
		t.Fatalf("supplied-client lookup = %#v, %v", got, err)
	}
	second := &catalogTransport{roundTrip: func(*http.Request) (*http.Response, error) {
		calls++
		return catalogResponse(strings.Replace(catalogJSON, `"is_deprecated":false`, `"is_deprecated":true`, 1)), nil
	}}
	client.Transport = second
	want := catalogPackage()
	want.Deprecated = true
	got, err = reader.GetPackage(context.Background(), want.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("client reuse or fresh metadata lost = %#v, %v; want %#v", got, err, want)
	}
	second.roundTrip = func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://example.invalid/redirect"}},
			Body: io.NopCloser(strings.NewReader("redirect")),
		}, nil
	}
	got, err = reader.GetPackage(context.Background(), want.ID)
	requirePackageError(t, got, err, want.ID, warden.ErrRemoteFailure, thunderstore.ErrThunderstoreAPI)
	if redirectCalls != 1 || !strings.Contains(err.Error(), "HTTP 302") {
		t.Errorf("caller redirect policy was not retained: redirects=%d, error=%v", redirectCalls, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if client.Timeout != time.Minute || client.Transport != second || client.Jar != jar ||
		first.idleCloses != 0 || second.idleCloses != 0 || calls != 3 {
		t.Errorf("caller client changed: timeout=%v, transport=%T, jar=%p, idle closes=%d/%d, requests=%d",
			client.Timeout, client.Transport, client.Jar, first.idleCloses, second.idleCloses, calls)
	}
}

func TestGetPackageClosedReader(t *testing.T) {
	reader := openCatalogReader(t, catalogClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("closed reader made an HTTP request")
		return nil, nil
	}))
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	for _, closed := range []*warden.Reader{reader, new(warden.Reader)} {
		for _, id := range []warden.PackageID{catalogPackage().ID, {}} {
			got, err := closed.GetPackage(context.Background(), id)
			requirePackageError(t, got, err, id, warden.ErrClosed)
			if errors.Is(err, warden.ErrInvalidPackageID) {
				t.Errorf("identity validation overrode the closed state: %v", err)
			}
		}
	}
}

func TestGetPackageSharedCalls(t *testing.T) {
	var calls atomic.Int32
	reader := openCatalogReader(t, catalogClient(t, func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return catalogResponse(catalogJSON), nil
	}))
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 2; j++ {
				want := catalogPackage()
				got, err := reader.GetPackage(context.Background(), want.ID)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("concurrent lookup = %#v, %v", got, err)
					return
				}
				got.Latest.Dependencies[0].DeclaredVersion = "caller-owned"
			}
		}()
	}
	workers.Wait()
	if calls.Load() != 16 {
		t.Errorf("concurrent metadata calls = %d; want 16", calls.Load())
	}
}

func TestGetPackageSilence(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "catalog-output")
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
	status, body := http.StatusOK, catalogJSON
	reader := openCatalogReader(t, catalogClient(t, func(*http.Request) (*http.Response, error) {
		response := catalogResponse(body)
		response.StatusCode = status
		return response, nil
	}))
	for _, tc := range []struct {
		status int
		body   string
		class  error
	}{
		{http.StatusOK, catalogJSON, nil}, {http.StatusOK, "{}", warden.ErrInvalidMetadata},
		{http.StatusOK, "{", warden.ErrInvalidMetadata}, {http.StatusNotFound, "not found", warden.ErrPackageNotFound},
		{http.StatusBadGateway, "failed", warden.ErrRemoteFailure},
	} {
		status, body = tc.status, tc.body
		got, err := reader.GetPackage(context.Background(), catalogPackage().ID)
		if tc.class != nil {
			requirePackageError(t, got, err, catalogPackage().ID, tc.class)
		} else if err != nil || !reflect.DeepEqual(got, catalogPackage()) {
			t.Errorf("successful quiet lookup = %#v, %v", got, err)
		}
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = stdout, stderr
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Errorf("catalog operations printed output: %q", data)
	}
}

type catalogBody struct {
	io.Reader
	close func() error
}

func (b *catalogBody) Close() error { return b.close() }

type catalogReadFunc func([]byte) (int, error)

func (f catalogReadFunc) Read(p []byte) (int, error) { return f(p) }

type catalogTransport struct {
	roundTrip  func(*http.Request) (*http.Response, error)
	idleCloses int
}

func (t *catalogTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.roundTrip(req)
}

func (t *catalogTransport) CloseIdleConnections() { t.idleCloses++ }
