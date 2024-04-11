package command

import (
	"errors"
	"fmt"
	"warden/api"
	"warden/api/thunderstore"
	"warden/data/repo"
)

func parseThunderstoreAPIError(err error) {
	if errors.Is(err, thunderstore.ErrPackageNotFound) {
		fmt.Println("... unable to find mod package...")
	} else if errors.Is(err, thunderstore.ErrThunderstoreAPI) {
		fmt.Println("... Thunderstore.io is experiencing issues. Please try again later ...")
	} else if errors.Is(err, api.ErrByteIO) || errors.Is(err, api.ErrHTTPClient) || errors.Is(err, api.ErrJSONParse) {
		fmt.Println("... unexpected error processing Thunderstore.io request ...")
	}
}

func parseRepoError(err error) {
	if errors.Is(err, repo.ErrModFetchNoResults) {
		fmt.Println("... that mod is currently not installed ...")
	} else if errors.Is(err, repo.ErrModFetchFailed) {
		fmt.Println("... unable to find mod ...")
	}
}
