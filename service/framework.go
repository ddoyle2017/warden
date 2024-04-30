package service

import (
	"bufio"
	"errors"
	"io"
	"warden/api/thunderstore"
	"warden/config"
	"warden/data/file"
	"warden/data/repo"
	"warden/domain/framework"
)

var (
	ErrUnableToInstallFramework = errors.New("unable to install mod framework")
)

type FrameworkService interface {
	InstallBepInEx() error
}

type frameworkService struct {
	c  config.Config
	r  repo.Frameworks
	fm file.Manager
	ts thunderstore.Thunderstore
	in *bufio.Scanner
}

func NewFrameworkService(c config.Config, r repo.Frameworks, fm file.Manager, ts thunderstore.Thunderstore, reader io.Reader) FrameworkService {
	return &frameworkService{
		c:  c,
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

	// Install BepInEx
	pkg, err := fs.ts.GetPackage(framework.BepInExNamespace, framework.BepInEx)
	if err != nil {
		return ErrUnableToInstallFramework
	}

	_, err = fs.fm.InstallBepInEx(pkg.Latest.DownloadURL, pkg.Latest.FullName)
	if err != nil {
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
	return nil
}
