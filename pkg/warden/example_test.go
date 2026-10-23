package warden_test

import (
	"context"
	"fmt"

	"github.com/ddoyle2017/warden/pkg/warden"
)

func ExampleReader_ListInstalledMods() {
	ctx := context.Background()
	reader, err := warden.Open(ctx, warden.Options{DatabasePath: "server-data/warden.db"})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() {
		if err := reader.Close(); err != nil {
			fmt.Println(err)
		}
	}()

	mods, err := reader.ListInstalledMods(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, mod := range mods {
		fmt.Printf("%s/%s %s at %s\n", mod.Package.Namespace, mod.Package.Name, mod.Version, mod.InstallPath)
	}
}

func ExampleReader_GetPackage() {
	ctx := context.Background()
	reader, err := warden.Open(ctx, warden.Options{DatabasePath: "server-data/warden.db"})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() {
		if err := reader.Close(); err != nil {
			fmt.Println(err)
		}
	}()

	pkg, err := reader.GetPackage(ctx, warden.PackageID{Namespace: "Azumatt", Name: "Where_You_At"})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%s/%s latest: %s\n", pkg.ID.Namespace, pkg.ID.Name, pkg.Latest.Version)
	for _, dep := range pkg.Latest.Dependencies {
		fmt.Printf("declares %s/%s %s\n", dep.Package.Namespace, dep.Package.Name, dep.DeclaredVersion)
	}
}

func ExampleReader_CheckUpdates() {
	ctx := context.Background()
	reader, err := warden.Open(ctx, warden.Options{DatabasePath: "server-data/warden.db"})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() {
		if err := reader.Close(); err != nil {
			fmt.Println(err)
		}
	}()

	checks, err := reader.CheckUpdates(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, check := range checks {
		fmt.Printf("%s/%s: %s (installed %s, latest %s)\n",
			check.Package.Namespace, check.Package.Name, check.Status, check.InstalledVersion, check.LatestVersion)
	}
}
