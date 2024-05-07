package service

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
)

var (
	ErrUnableToInstallFramework = errors.New("unable to install mod framework")
	ErrUnableToRemoveFramework  = errors.New("unable to remove mod framework")
	ErrFrameworkNotFound        = errors.New("unable to delete record from frameworks table")
)

type FrameworkService interface {
	InstallBepInEx() error
	RemoveBepInEx() error
}

type frameworkService struct {
	r  repo.Frameworks
	fm file.Manager
	ts thunderstore.Thunderstore
	in *bufio.Scanner
}

func NewFrameworkService(r repo.Frameworks, fm file.Manager, ts thunderstore.Thunderstore, reader io.Reader) FrameworkService {
	return &frameworkService{
		r:  r,
		fm: fm,
		ts: ts,
		in: bufio.NewScanner(reader),
	}
}

func (fs *frameworkService) InstallBepInEx() error {
	// Check if BepInEx already installed
	if _, err := fs.r.GetFramework(framework.BepInEx); err == nil {
		return nil
	}

	fmt.Println("... BepInEx installation is missing ...")
	fmt.Printf("did you want to install BepInEx? %s\n", yesOrNo)

	tries := 0
	for fs.in.Scan() && tries < 2 {
		if fs.in.Text() == yes {
			// Install BepInEx
			pkg, err := fs.ts.GetPackage(framework.BepInExNamespace, framework.BepInEx)
			if err != nil {
				return ErrFrameworkNotFound
			}

			_, err = fs.fm.InstallBepInEx(pkg.Latest.DownloadURL, pkg.Latest.FullName)
			if err != nil {
				fmt.Printf("%+v\n", err)
				return ErrUnableToInstallFramework
			}

			f := framework.Framework{
				Name:        pkg.Latest.Name,
				Namespace:   pkg.Latest.Namespace,
				Version:     pkg.Latest.VersionNumber,
				WebsiteURL:  pkg.Latest.WebsiteURL,
				Description: pkg.Latest.Description,
			}
			err = fs.r.InsertFramework(f)
			if err != nil {
				return ErrUnableToInstallFramework
			}

			fmt.Println("... successfully installed BepInEx ...")
			return nil
		} else if fs.in.Text() == no {
			fmt.Println("... aborting ...")
			return nil
		} else {
			tries++
		}
	}
	if tries >= 2 {
		return ErrMaxAttempts
	}
	return nil
}

func (fs *frameworkService) RemoveBepInEx() error {
	fmt.Printf("are you sure you want to remove BepInEx? %s\n", yesOrNoLong)

	tries := 0
	for fs.in.Scan() && tries < 2 {
		if fs.in.Text() == yesLong {
			if err := fs.fm.RemoveBepInEx(); err != nil {
				return ErrUnableToRemoveFramework
			}
			if err := fs.r.DeleteFramework(framework.BepInEx); err != nil {
				return ErrUnableToRemoveFramework
			}
			return nil
		} else if fs.in.Text() == noLong {
			fmt.Println("... aborting ...")
			return nil
		} else {
			tries++
		}
	}
	if tries >= 2 {
		return ErrMaxAttempts
	}
	return nil
}
