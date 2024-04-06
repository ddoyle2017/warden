package thunderstore

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	thunderstoreAPI = "https://thunderstore.io/api"
	experimental    = "/experimental"
	packageAPI      = "/package"
)

// Interface for Thunderstore's API for Valheim mods. See docs: https://thunderstore.io/c/valheim/create/docs/
type Thunderstore interface {
	GetPackage(namespace, name string) (Package, error)
}

type api struct {
	client *http.Client
}

func New(c *http.Client) Thunderstore {
	return &api{
		client: c,
	}
}

func (a *api) GetPackage(namespace, name string) (Package, error) {
	url := fmt.Sprintf(thunderstoreAPI+experimental+packageAPI+"/%s/%s", namespace, name)
	response, err := a.client.Get(url)
	if err != nil {
		fmt.Printf("... ERROR: unable to find %s by %s...", name, namespace)
		return Package{}, err
	}

	data, err := io.ReadAll(response.Body)
	if err != nil {
		fmt.Println("... ERROR: unable to parse Thunderstore API response...")
		return Package{}, err
	}

	pkg := Package{}
	err = json.Unmarshal(data, &pkg)
	if err != nil {
		fmt.Println("... ERROR: unable to deserialize Thunderstore API response from JSON...")
	}

	return pkg, nil
}
