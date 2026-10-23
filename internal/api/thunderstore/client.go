package thunderstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/api"
	"io"
	"net/http"
	"net/url"
)

const (
	thunderstoreAPI = "https://thunderstore.io/api"
	experimental    = "/experimental"
	packageAPI      = "/package"
)

var (
	ErrPackageNotFound = errors.New("mod package was not found")
	ErrThunderstoreAPI = errors.New("Thunderstore API returned an unexpected error")
)

// Thunderstore is the metadata interface used by mutation services.
type Thunderstore interface {
	GetPackage(namespace, name string) (Package, error)
	GetRelease(namespace, name, version string) (Release, error)
}

// HTTPClient performs metadata requests with net/http.Client.Do semantics.
// On error, any returned response body is already closed by the transport.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Client retrieves Thunderstore metadata without downloading archives or printing
// output. Errors retain API classifications, request identity, and original causes.
type Client struct {
	client HTTPClient
}

func New(c HTTPClient) *Client {
	return &Client{
		client: c,
	}
}

func (ts *Client) GetPackage(namespace, name string) (Package, error) {
	return ts.GetPackageContext(context.Background(), namespace, name)
}

// GetPackageContext fetches package metadata using ctx for the request and body read.
// It decodes the response without validating complete package values.
func (ts *Client) GetPackageContext(ctx context.Context, namespace, name string) (Package, error) {
	endpoint := fmt.Sprintf(thunderstoreAPI+experimental+packageAPI+"/%s/%s",
		url.PathEscape(namespace), url.PathEscape(name))

	var pkg Package
	if err := ts.getJSON(ctx, endpoint, &pkg); err != nil {
		return Package{}, err
	}
	return pkg, nil
}

func (ts *Client) GetRelease(namespace, name, version string) (Release, error) {
	return ts.GetReleaseContext(context.Background(), namespace, name, version)
}

// GetReleaseContext requires matching release identity and an explicit dependency list.
func (ts *Client) GetReleaseContext(ctx context.Context, namespace, name, version string) (Release, error) {
	endpoint := fmt.Sprintf(thunderstoreAPI+experimental+packageAPI+"/%s/%s/%s/",
		url.PathEscape(namespace), url.PathEscape(name), url.PathEscape(version))

	var release Release
	if err := ts.getJSON(ctx, endpoint, &release); err != nil {
		return Release{}, err
	}
	if release.Namespace != namespace || release.Name != name || release.VersionNumber != version ||
		release.FullName != namespace+"-"+name+"-"+version {
		return Release{}, fmt.Errorf("GET %s: %w: expected release %s/%s@%s, got %s/%s@%s (%q)",
			endpoint, ErrThunderstoreAPI, namespace, name, version, release.Namespace, release.Name, release.VersionNumber, release.FullName)
	}
	if release.Dependencies == nil {
		return Release{}, fmt.Errorf("GET %s: %w: release %s/%s@%s is missing its dependency list",
			endpoint, ErrThunderstoreAPI, namespace, name, version)
	}
	return release, nil
}

func (ts *Client) getJSON(ctx context.Context, endpoint string, target any) (err error) {
	var response *http.Response
	defer func() {
		if response != nil && response.Body != nil {
			if closeErr := response.Body.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("%w: close response body: %w", api.ErrByteIO, closeErr))
			}
		}
		if ctx != nil && ctx.Err() != nil {
			for _, cause := range []error{ctx.Err(), context.Cause(ctx)} {
				if cause != nil && !errors.Is(err, cause) {
					if err == nil {
						err = api.ErrHTTPClient
					}
					err = errors.Join(err, cause)
				}
			}
		}
		if err != nil {
			if response != nil {
				err = fmt.Errorf("HTTP %d: %w", response.StatusCode, err)
			}
			err = fmt.Errorf("%s %s: %w", http.MethodGet, endpoint, err)
		}
	}()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", api.ErrHTTPClient, err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", api.ErrHTTPClient, err)
	}
	result, err := ts.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %w", api.ErrHTTPClient, err)
	}
	response = result
	if response == nil {
		return fmt.Errorf("%w: missing response", api.ErrHTTPClient)
	}
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		err = ErrPackageNotFound
	default:
		err = ErrThunderstoreAPI
	}
	if response.Body == nil {
		return errors.Join(err, fmt.Errorf("%w: missing response body", api.ErrHTTPClient))
	}

	data, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		return errors.Join(err, fmt.Errorf("%w: read response body: %w", api.ErrByteIO, readErr))
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("%w: %w", api.ErrJSONParse, err)
	}
	return nil
}
