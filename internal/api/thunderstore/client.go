package thunderstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"warden/internal/api"
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

// Interface for Thunderstore's API for Valheim mods. See docs: https://thunderstore.io/c/valheim/create/docs/
type Thunderstore interface {
	GetPackage(namespace, name string) (Package, error)
	GetRelease(namespace, name, version string) (Release, error)
}

type thunderstore struct {
	client api.HTTPClient
}

func New(c api.HTTPClient) Thunderstore {
	return &thunderstore{
		client: c,
	}
}

func (ts *thunderstore) GetPackage(namespace, name string) (Package, error) {
	url := fmt.Sprintf(thunderstoreAPI+experimental+packageAPI+"/%s/%s", namespace, name)

	response, err := ts.client.Get(url)
	if err != nil {
		return Package{}, api.ErrHTTPClient
	}
	defer response.Body.Close()

	data, err := io.ReadAll(response.Body)
	if err != nil {
		return Package{}, api.ErrByteIO
	}

	switch response.StatusCode {
	case http.StatusOK:
		pkg, err := deserializeJSON(data, Package{})
		if err != nil {
			return Package{}, api.ErrJSONParse
		}
		return pkg, nil
	case http.StatusNotFound:
		// API currently doesn't return any useful data, so we'll ignore the error response body for now
		return Package{}, ErrPackageNotFound
	default:
		// API currently doesn't return any useful data, so we'll ignore the error response body for now
		return Package{}, ErrThunderstoreAPI
	}
}

func (ts *thunderstore) GetRelease(namespace, name, version string) (Release, error) {
	endpoint := fmt.Sprintf(thunderstoreAPI+experimental+packageAPI+"/%s/%s/%s/",
		url.PathEscape(namespace), url.PathEscape(name), url.PathEscape(version))
	response, err := ts.client.Get(endpoint)
	if err != nil {
		return Release{}, fmt.Errorf("%w: %w", api.ErrHTTPClient, err)
	}
	data, readErr := io.ReadAll(response.Body)
	if err := errors.Join(readErr, response.Body.Close()); err != nil {
		return Release{}, fmt.Errorf("%w: %w", api.ErrByteIO, err)
	}
	switch response.StatusCode {
	case http.StatusOK:
		var release Release
		if err := json.Unmarshal(data, &release); err != nil {
			return Release{}, fmt.Errorf("%w: %w", api.ErrJSONParse, err)
		}
		if release.Namespace != namespace || release.Name != name || release.VersionNumber != version ||
			release.FullName != namespace+"-"+name+"-"+version {
			return Release{}, fmt.Errorf("%w: expected release %s/%s@%s, got %s/%s@%s (%q)",
				ErrThunderstoreAPI, namespace, name, version, release.Namespace, release.Name, release.VersionNumber, release.FullName)
		}
		if release.Dependencies == nil {
			return Release{}, fmt.Errorf("%w: release %s/%s@%s is missing its dependency list",
				ErrThunderstoreAPI, namespace, name, version)
		}
		return release, nil
	case http.StatusNotFound:
		return Release{}, ErrPackageNotFound
	default:
		return Release{}, fmt.Errorf("%w: HTTP %d", ErrThunderstoreAPI, response.StatusCode)
	}
}

func deserializeJSON[T any](data []byte, obj T) (T, error) {
	err := json.Unmarshal(data, &obj)
	if err != nil {
		fmt.Println("... ERROR: unable to deserialize Thunderstore API response from JSON...")
		return obj, err
	}
	return obj, nil
}
