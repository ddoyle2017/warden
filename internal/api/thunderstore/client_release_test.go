package thunderstore_test

import (
	"errors"
	"github.com/ddoyle2017/warden/internal/api"
	"github.com/ddoyle2017/warden/internal/api/thunderstore"
	"github.com/ddoyle2017/warden/internal/test/mock"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
)

func TestGetRelease(t *testing.T) {
	const metadata = `{"namespace":"Owner","name":"Root","version_number":"1.0.0","full_name":"Owner-Root-1.0.0","dependencies":["Required-Dependency-2.0.0"],"description":"original release"}`
	transportCause, closeCause := errors.New("transport interrupted"), errors.New("body close interrupted")
	for name, tt := range map[string]struct {
		body, version                   string
		status                          int
		transport, read, close, wantErr error
		empty                           bool
	}{
		"an exact release": {body: metadata},
		"an explicit empty dependency list": {
			body: strings.Replace(metadata, `["Required-Dependency-2.0.0"]`, `[]`, 1), empty: true,
		},
		"a complete prerelease and build suffix": {
			body: strings.ReplaceAll(metadata, "1.0.0", "1.0.0-rc.1+linux-x64.001"), version: "1.0.0-rc.1+linux-x64.001",
		},
		"a missing version": {
			body: `{"detail":"Not found."}`, status: http.StatusNotFound, wantErr: thunderstore.ErrPackageNotFound,
		},
		"an API failure": {
			body: `{}`, status: http.StatusInternalServerError, wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"a transport failure": {
			transport: transportCause, wantErr: api.ErrHTTPClient,
		},
		"a body read failure": {
			read: io.ErrUnexpectedEOF, wantErr: api.ErrByteIO,
		},
		"a body close failure": {
			body: metadata, close: closeCause, wantErr: api.ErrByteIO,
		},
		"read and close failures": {
			read: io.ErrUnexpectedEOF, close: closeCause, wantErr: api.ErrByteIO,
		},
		"invalid JSON": {
			body: `{`, wantErr: api.ErrJSONParse,
		},
		"another namespace": {
			body: strings.ReplaceAll(metadata, "Owner", "Other"), wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"another package": {
			body: strings.ReplaceAll(metadata, "Root", "Wrong"), wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"a changed latest version": {
			body: strings.ReplaceAll(metadata, "1.0.0", "2.0.0"), wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"a mismatched namespace field with a matching full name": {
			body: strings.Replace(metadata, `"namespace":"Owner"`, `"namespace":"Other"`, 1), wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"a mismatched name field with a matching full name": {
			body: strings.Replace(metadata, `"name":"Root"`, `"name":"Wrong"`, 1), wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"a mismatched version field with a matching full name": {
			body: strings.Replace(metadata, `"version_number":"1.0.0"`, `"version_number":"2.0.0"`, 1), wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"a mismatched full name": {
			body: strings.Replace(metadata, "Owner-Root-1.0.0", "Owner-Root-2.0.0", 1), wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"a missing dependency list": {
			body: strings.Replace(metadata, `"dependencies":["Required-Dependency-2.0.0"],`, "", 1), wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"a null dependency list": {
			body: strings.Replace(metadata, `["Required-Dependency-2.0.0"]`, `null`, 1), wantErr: thunderstore.ErrThunderstoreAPI,
		},
		"a non-list dependency field": {
			body: strings.Replace(metadata, `["Required-Dependency-2.0.0"]`, `"Required-Dependency-2.0.0"`, 1), wantErr: api.ErrJSONParse,
		},
	} {
		t.Run("given "+name+", when fetching an installed version, then require complete matching metadata", func(t *testing.T) {
			version, status := tt.version, tt.status
			if version == "" {
				version = "1.0.0"
			}
			if status == 0 {
				status = http.StatusOK
			}
			calls, closes := 0, 0
			client := &mock.HTTPClient{GetFunc: func(endpoint string) (*http.Response, error) {
				calls++
				if endpoint != "https://thunderstore.io/api/experimental/package/Owner/Root/"+version+"/" {
					t.Fatalf("wrong version endpoint %q", endpoint)
				}
				if tt.transport != nil {
					return nil, tt.transport
				}
				var reader io.Reader = strings.NewReader(tt.body)
				if tt.read != nil {
					reader = iotest.ErrReader(tt.read)
				}
				return &http.Response{StatusCode: status, Body: &releaseResponseBody{
					Reader: reader, close: func() error { closes++; return tt.close },
				}}, nil
			}}
			release, err := thunderstore.New(client).GetRelease("Owner", "Root", version)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			for _, cause := range []error{tt.transport, tt.read, tt.close} {
				if cause != nil && !errors.Is(err, cause) {
					t.Errorf("underlying cause %v was lost: %v", cause, err)
				}
			}
			want := thunderstore.Release{}
			if tt.wantErr == nil {
				want = thunderstore.Release{
					Namespace: "Owner", Name: "Root", VersionNumber: version, FullName: "Owner-Root-" + version,
					Dependencies: []string{"Required-Dependency-2.0.0"}, Description: "original release",
				}
				if tt.empty {
					want.Dependencies = []string{}
				}
				if release.Dependencies == nil {
					t.Error("successful metadata lost its explicit dependency list")
				}
			}
			if !release.Equals(&want) {
				t.Errorf("release = %+v, want %+v", release, want)
			}
			wantCloses := 1
			if tt.transport != nil {
				wantCloses = 0
			}
			if calls != 1 || closes != wantCloses {
				t.Errorf("requests/closes = %d/%d, want 1/%d", calls, closes, wantCloses)
			}
		})
	}
}

type releaseResponseBody struct {
	io.Reader
	close func() error
}

func (b *releaseResponseBody) Close() error { return b.close() }
