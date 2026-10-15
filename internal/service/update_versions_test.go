package service_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"warden/internal/api/thunderstore"
	"warden/internal/data/file"
	"warden/internal/data/repo"
	"warden/internal/domain/framework"
	"warden/internal/domain/mod"
	"warden/internal/service"
	"warden/internal/test/helper"
	"warden/internal/test/mock"
)

func TestUpdateReleaseVersions(t *testing.T) {
	operations := []struct {
		name      string
		framework bool
		run       func(service.Mod, service.Framework) error
	}{
		{"UpdateMod", false, func(ms service.Mod, _ service.Framework) error { return ms.UpdateMod("Versioned") }},
		{"UpdateModByIdentity", false, func(ms service.Mod, _ service.Framework) error { return ms.UpdateModByIdentity("Owned", "Versioned") }},
		{"UpdateAllMods", false, func(ms service.Mod, _ service.Framework) error { return ms.UpdateAllMods() }},
		{"UpdateBepInEx", true, func(_ service.Mod, fs service.Framework) error { return fs.UpdateBepInEx() }},
	}
	tests := map[string]struct {
		installed, latest string
		updated           bool
		invalid           string
		answer            string
		wantErr           error
	}{
		"given an ordinary numeric upgrade, when updating, then install the newer release": {
			installed: "1.0.0", latest: "1.1.0", updated: true,
		},
		"given a multi-digit minor upgrade, when updating, then install the newer release": {
			installed: "1.9.0", latest: "1.10.0", updated: true,
		},
		"given a multi-digit major upgrade, when updating, then install the newer release": {
			installed: "9.9.9", latest: "10.0.0", updated: true,
		},
		"given a multi-digit patch upgrade, when updating, then install the newer release": {
			installed: "1.0.9", latest: "1.0.10", updated: true,
		},
		"given a lower minor version, when updating, then refuse the downgrade": {
			installed: "1.10.0", latest: "1.9.0",
		},
		"given a lower major with a higher minor, when updating, then refuse the downgrade": {
			installed: "2.0.0", latest: "1.99.99",
		},
		"given equal versions, when updating, then preserve the installation": {
			installed: "1.10.0", latest: "1.10.0",
		},
		"given a newer release, when declining confirmation, then preserve the installation": {
			installed: "1.9.0", latest: "1.10.0", answer: "n\n",
		},
		"given a newer release, when confirmation attempts are exhausted, then fail without mutation": {
			installed: "1.9.0", latest: "1.10.0", answer: "invalid\ninvalid\ninvalid\n", wantErr: service.ErrMaxAttempts,
		},
		"given a framework-style patch version, when updating, then install the newer release": {
			installed: "5.4.2202", latest: "5.4.2203", updated: true,
		},
		"given a prerelease and its stable release, when updating, then install the stable release": {
			installed: "1.0.0-rc.1", latest: "1.0.0", updated: true,
		},
		"given a stable release and its prerelease, when updating, then refuse the downgrade": {
			installed: "1.0.0", latest: "1.0.0-rc.1",
		},
		"given a numeric prerelease upgrade, when updating, then compare its identifiers numerically": {
			installed: "1.0.0-beta.9", latest: "1.0.0-beta.10", updated: true,
		},
		"given a lower numeric prerelease, when updating, then refuse the downgrade": {
			installed: "1.0.0-beta.10", latest: "1.0.0-beta.9",
		},
		"given an alphabetic prerelease upgrade, when updating, then install it": {
			installed: "1.0.0-alpha", latest: "1.0.0-beta", updated: true,
		},
		"given a numeric and an alphabetic prerelease, when updating, then prefer the alphabetic identifier": {
			installed: "1.0.0-9", latest: "1.0.0-alpha", updated: true,
		},
		"given a longer matching prerelease, when updating, then install it": {
			installed: "1.0.0-alpha", latest: "1.0.0-alpha.1", updated: true,
		},
		"given only different build metadata, when updating, then preserve the installation": {
			installed: "1.0.0+build.1", latest: "1.0.0+build.2",
		},
		"given an upgrade with build metadata, when updating, then preserve the complete release identity": {
			installed: "1.0.9+old", latest: "1.0.10+new.001", updated: true,
		},
		"given a prerelease upgrade with build metadata, when updating, then preserve both suffixes": {
			installed: "1.0.0-beta.9+old", latest: "1.0.0-beta.10+new.001", updated: true,
		},
		"given numeric components beyond uint64, when updating, then order them without overflow": {
			installed: "18446744073709551615.0.0", latest: "18446744073709551616.0.0", updated: true,
		},
		"given an empty installed version, when updating, then fail without mutation": {
			latest: "1.1.0", invalid: "installed",
		},
		"given a malformed installed version, when updating, then fail without mutation": {
			installed: "broken", latest: "1.1.0", invalid: "installed",
		},
		"given an abbreviated installed version, when updating, then fail without mutation": {
			installed: "1.0", latest: "1.1.0", invalid: "installed",
		},
		"given an empty latest version, when updating, then fail without mutation": {
			installed: "1.0.0", invalid: "latest",
		},
		"given a malformed latest version, when updating, then fail without mutation": {
			installed: "1.0.0", latest: "broken", invalid: "latest",
		},
		"given an abbreviated latest version, when updating, then fail without mutation": {
			installed: "1.0.0", latest: "1.2", invalid: "latest",
		},
		"given a v-prefixed latest version, when updating, then fail without mutation": {
			installed: "1.0.0", latest: "v1.1.0", invalid: "latest",
		},
		"given a four-component version, when updating, then fail without mutation": {
			installed: "1.0.0", latest: "1.1.0.1", invalid: "latest",
		},
		"given a leading zero in the version core, when updating, then fail without mutation": {
			installed: "1.0.0", latest: "1.01.0", invalid: "latest",
		},
		"given a leading zero in a numeric prerelease, when updating, then fail without mutation": {
			installed: "1.0.0", latest: "1.1.0-01", invalid: "latest",
		},
		"given an empty prerelease identifier, when updating, then fail without mutation": {
			installed: "1.0.0", latest: "1.1.0-alpha..1", invalid: "latest",
		},
		"given empty build metadata, when updating, then fail without mutation": {
			installed: "1.0.0", latest: "1.1.0+", invalid: "latest",
		},
		"given whitespace in a version, when updating, then fail without mutation": {
			installed: "1.0.0", latest: " 1.1.0", invalid: "latest",
		},
		"given equal malformed versions, when updating, then fail rather than treating them as current": {
			installed: "broken", latest: "broken", invalid: "installed",
		},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			for name, tt := range tests {
				t.Run(name, func(t *testing.T) {
					server := filepath.Join(t.TempDir(), "server")
					th := helper.NewHelper(t, helper.WithValheimFolder(server))
					t.Setenv("TMPDIR", t.TempDir())
					db := th.CreateDatabase()
					repo.CreateFrameworksTable(db)
					repo.CreateModsTable(db)
					modRecords, frameworkRecords := repo.NewModsRepo(db), repo.NewFrameworksRepo(db)
					oldFramework := framework.Framework{
						Name: framework.BepInEx, Namespace: framework.BepInExNamespace, Version: "5.4.2202",
					}
					oldMod := mod.Mod{Name: "Versioned", Namespace: "Owned", Version: "1.0.0", FrameworkID: 1}
					if operation.framework {
						oldFramework.Version = tt.installed
					} else {
						oldMod.Version = tt.installed
					}
					pluginPath := filepath.Join("BepInEx", "plugins", oldMod.FullName(), "plugin.dll")
					oldMod.FilePath = filepath.Join(server, filepath.Dir(pluginPath))
					wantFiles := map[string]string{
						pluginPath:                  "installed plugin",
						"BepInEx/core/runtime":      "installed framework",
						"BepInEx/config/server.cfg": "user configuration",
						"server-data":               "unrelated server bytes",
					}
					for path, contents := range wantFiles {
						fullPath := filepath.Join(server, path)
						if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(fullPath, []byte(contents), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					if err := frameworkRecords.InsertFramework(oldFramework); err != nil {
						t.Fatal(err)
					}
					if err := modRecords.InsertMod(oldMod); err != nil {
						t.Fatal(err)
					}
					oldMod, err := modRecords.GetMod(oldMod.Name)
					if err != nil {
						t.Fatal(err)
					}
					oldFramework, err = frameworkRecords.GetFramework(framework.BepInEx)
					if err != nil {
						t.Fatal(err)
					}
					latest := thunderstore.Release{
						Namespace: oldMod.Namespace, Name: oldMod.Name, VersionNumber: tt.latest,
						DownloadURL: helper.TestDownloadURL, Description: "replacement description",
					}
					archivePath, replacement := "plugin.dll", "replacement plugin"
					if operation.framework {
						latest.Namespace, latest.Name = framework.BepInExNamespace, framework.BepInEx
						archivePath, replacement = "BepInExPack_Valheim/BepInEx/core/runtime", "replacement framework"
					}
					latest.FullName = latest.Namespace + "-" + latest.Name + "-" + latest.VersionNumber
					var payload bytes.Buffer
					archive := zip.NewWriter(&payload)
					entry, err := archive.Create(archivePath)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := io.WriteString(entry, replacement); err != nil {
						t.Fatal(err)
					}
					if err := archive.Close(); err != nil {
						t.Fatal(err)
					}
					downloads, fileCalls, recordWrites := 0, 0, 0
					client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
						downloads++
						if url != latest.DownloadURL {
							t.Fatalf("unexpected download URL %q", url)
						}
						return &http.Response{Body: io.NopCloser(bytes.NewReader(payload.Bytes()))}, nil
					}}
					files := file.NewManager(client, server)
					fm := &mock.Manager{
						ValidateModInstallFunc: files.ValidateModInstall,
						ReplaceModFunc: func(url, oldFullName, newFullName string) (file.ModReplacement, error) {
							fileCalls++
							if oldFullName != oldMod.FullName() || url != latest.DownloadURL || newFullName != latest.FullName {
								t.Fatalf("unexpected replacement %q, %q, %q", url, oldFullName, newFullName)
							}
							return files.ReplaceMod(url, oldFullName, newFullName)
						},
						UpdateBepInExFunc: func(url, fullName string) error {
							fileCalls++
							if url != latest.DownloadURL || fullName != latest.FullName {
								t.Fatalf("unexpected framework release %q, %q", url, fullName)
							}
							return files.UpdateBepInEx(url, fullName)
						},
					}
					mr := &mock.ModsRepo{
						GetModFunc: modRecords.GetMod, ListModsFunc: modRecords.ListMods,
						GetModByIdentityFunc: modRecords.GetModByIdentity,
						UpsertModFunc: func(m mod.Mod) error {
							recordWrites++
							return modRecords.UpsertMod(m)
						},
					}
					fr := &mock.FrameworksRepo{
						GetFrameworkFunc: frameworkRecords.GetFramework,
						UpdateFrameworkFunc: func(f framework.Framework) error {
							recordWrites++
							return frameworkRecords.UpdateFramework(f)
						},
					}
					ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
						if namespace != latest.Namespace || name != latest.Name {
							t.Fatalf("unexpected package %q/%q", namespace, name)
						}
						return thunderstore.Package{Latest: latest}, nil
					}}
					answer := tt.answer
					if answer == "" {
						answer = "Y\n"
					}
					ms := service.NewModService(mr, fm, ts, strings.NewReader(answer))
					fs := service.NewFrameworkService(fr, fm, ts, strings.NewReader(answer))
					err = operation.run(ms, fs)
					if tt.invalid != "" {
						wantErr, invalidVersion := service.ErrUnableToUpdateMod, tt.latest
						if operation.framework {
							wantErr = service.ErrUnableToUpdateFramework
						}
						if tt.invalid == "installed" {
							invalidVersion = tt.installed
						}
						if !errors.Is(err, wantErr) || !errors.Is(err, service.ErrInvalidReleaseVersion) {
							t.Errorf("expected update failure %v, got %v", wantErr, err)
						}
						if err != nil && (!strings.Contains(err.Error(), tt.invalid) || !strings.Contains(err.Error(), fmt.Sprintf("%q", invalidVersion))) {
							t.Errorf("error does not identify the invalid %s version %q: %v", tt.invalid, invalidVersion, err)
						}
						if err != nil && !strings.Contains(err.Error(), latest.Namespace+"/"+latest.Name) {
							t.Errorf("error does not identify the package: %v", err)
						}
					} else if !errors.Is(err, tt.wantErr) {
						t.Errorf("update error = %v, want %v", err, tt.wantErr)
					}
					wantMod, wantFramework := oldMod, oldFramework
					wantDownloads, wantFileCalls, wantRecordWrites := 0, 0, 0
					if tt.updated {
						wantDownloads, wantRecordWrites = 1, 1
						if operation.framework {
							wantFileCalls = 1
							wantFramework.Version, wantFramework.Description = tt.latest, latest.Description
							wantFiles["BepInEx/core/runtime"] = replacement
						} else {
							wantFileCalls = 1
							wantMod.Version, wantMod.Description = tt.latest, latest.Description
							wantMod.FilePath = filepath.Join(server, "BepInEx", "plugins", latest.FullName)
							delete(wantFiles, pluginPath)
							wantFiles[filepath.Join("BepInEx", "plugins", latest.FullName, "plugin.dll")] = replacement
						}
					}
					if downloads != wantDownloads || fileCalls != wantFileCalls || recordWrites != wantRecordWrites {
						t.Errorf("downloads/file calls/record writes = %d/%d/%d, want %d/%d/%d",
							downloads, fileCalls, recordWrites, wantDownloads, wantFileCalls, wantRecordWrites)
					}
					gotMod, err := modRecords.GetMod(oldMod.Name)
					if err != nil || !reflect.DeepEqual(gotMod, wantMod) {
						t.Errorf("mod record = %+v, %v; want %+v", gotMod, err, wantMod)
					}
					gotFramework, err := frameworkRecords.GetFramework(framework.BepInEx)
					if err != nil || gotFramework != wantFramework {
						t.Errorf("framework record = %+v, %v; want %+v", gotFramework, err, wantFramework)
					}
					paths, err := th.GetFileList(server)
					if err != nil {
						t.Fatal(err)
					}
					gotFiles := make(map[string]string)
					for relative, path := range paths {
						data, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						gotFiles[relative] = string(data)
					}
					if !reflect.DeepEqual(gotFiles, wantFiles) {
						t.Errorf("server files = %q, want %q", gotFiles, wantFiles)
					}
				})
			}
		})
	}
}

func TestUpdateAllMods_InvalidVersionPreservesPartialProgress(t *testing.T) {
	for name, tt := range map[string]struct {
		installed, latest, invalid string
	}{
		"given an invalid installed version after an upgrade, when updating all, then keep progress and stop": {
			installed: "broken", latest: "1.10.0", invalid: "installed",
		},
		"given an invalid latest version after an upgrade, when updating all, then keep progress and stop": {
			installed: "1.9.0", latest: "broken", invalid: "latest",
		},
	} {
		t.Run(name, func(t *testing.T) {
			th := helper.NewHelper(t)
			t.Setenv("TMPDIR", t.TempDir())
			server := th.GetValheimDirectory()
			db := th.CreateDatabase()
			repo.CreateFrameworksTable(db)
			repo.CreateModsTable(db)
			th.SeedFrameworksTable(repo.NewFrameworksRepo(db))
			records := repo.NewModsRepo(db)
			current := []mod.Mod{
				{Name: "Earlier", Namespace: "Owned", Version: "1.9.0", FrameworkID: 1},
				{Name: "Invalid", Namespace: "Owned", Version: tt.installed, FrameworkID: 1},
				{Name: "Later", Namespace: "Owned", Version: "1.9.0", FrameworkID: 1},
			}
			wantFiles := map[string]string{"server-data": "unrelated server bytes"}
			for i := range current {
				m := &current[i]
				pluginPath := filepath.Join("BepInEx", "plugins", m.FullName(), "plugin.dll")
				m.FilePath = filepath.Join(server, filepath.Dir(pluginPath))
				wantFiles[pluginPath] = "installed " + m.Name
				if err := records.InsertMod(*m); err != nil {
					t.Fatal(err)
				}
				stored, err := records.GetMod(m.Name)
				if err != nil {
					t.Fatal(err)
				}
				*m = stored
			}
			for path, contents := range wantFiles {
				fullPath := filepath.Join(server, path)
				if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(fullPath, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			latest := thunderstore.Release{
				Name: "Earlier", Namespace: "Owned", VersionNumber: "1.10.0",
				FullName: "Owned-Earlier-1.10.0", DownloadURL: helper.TestDownloadURL,
			}
			var payload bytes.Buffer
			archive := zip.NewWriter(&payload)
			entry, err := archive.Create("plugin.dll")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(entry, "replacement Earlier"); err != nil {
				t.Fatal(err)
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			var lookups, removed, installed, written []string
			downloads := 0
			client := &mock.HTTPClient{GetFunc: func(url string) (*http.Response, error) {
				downloads++
				if url != latest.DownloadURL {
					t.Fatalf("unexpected download URL %q", url)
				}
				return &http.Response{Body: io.NopCloser(bytes.NewReader(payload.Bytes()))}, nil
			}}
			files := file.NewManager(client, server)
			fm := &mock.Manager{
				ValidateModInstallFunc: files.ValidateModInstall,
				RemoveModFunc: func(fullName string) error {
					removed = append(removed, fullName)
					return files.RemoveMod(fullName)
				},
				InstallModFunc: func(url, fullName string) (string, error) {
					installed = append(installed, fullName)
					return files.InstallMod(url, fullName)
				},
			}
			mr := &mock.ModsRepo{
				// ListMods does not promise SQL row order; this scenario requires a fixed sequence.
				ListModsFunc: func() ([]mod.Mod, error) { return append([]mod.Mod(nil), current...), nil },
				UpsertModFunc: func(m mod.Mod) error {
					written = append(written, m.Name)
					return records.UpsertMod(m)
				},
			}
			ts := &mock.Thunderstore{GetPackageFunc: func(namespace, name string) (thunderstore.Package, error) {
				lookups = append(lookups, namespace+"/"+name)
				if namespace != "Owned" {
					t.Fatalf("unexpected namespace %q", namespace)
				}
				switch name {
				case "Earlier":
					return thunderstore.Package{Latest: latest}, nil
				case "Invalid":
					return thunderstore.Package{Latest: thunderstore.Release{
						Name: name, Namespace: namespace, VersionNumber: tt.latest,
						FullName: "Owned-Invalid-" + tt.latest, DownloadURL: helper.TestDownloadURL,
					}}, nil
				default:
					t.Fatalf("unexpected package lookup after invalid version: %q", name)
					return thunderstore.Package{}, nil
				}
			}}
			err = service.NewModService(mr, fm, ts, strings.NewReader("Y\n")).UpdateAllMods()
			if !errors.Is(err, service.ErrUnableToUpdateMod) || !errors.Is(err, service.ErrInvalidReleaseVersion) {
				t.Errorf("expected update and version errors, got %v", err)
			}
			for _, text := range []string{"Owned/Invalid", tt.invalid, `"broken"`} {
				if err == nil || !strings.Contains(err.Error(), text) {
					t.Errorf("error %v omits %q", err, text)
				}
			}
			if !reflect.DeepEqual(lookups, []string{"Owned/Earlier", "Owned/Invalid"}) {
				t.Errorf("package lookups = %v, want only Earlier then Invalid", lookups)
			}
			if !reflect.DeepEqual(removed, []string{"Owned-Earlier-1.9.0"}) ||
				!reflect.DeepEqual(installed, []string{"Owned-Earlier-1.10.0"}) ||
				!reflect.DeepEqual(written, []string{"Earlier"}) || downloads != 1 {
				t.Errorf("unexpected mutations: removed=%v installed=%v written=%v downloads=%d", removed, installed, written, downloads)
			}
			wantRecords := append([]mod.Mod(nil), current...)
			wantRecords[0].Version = "1.10.0"
			wantRecords[0].FilePath = filepath.Join(server, "BepInEx", "plugins", "Owned-Earlier-1.10.0")
			for _, want := range wantRecords {
				got, err := records.GetMod(want.Name)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("record for %s = %+v, %v; want %+v", want.Name, got, err, want)
				}
			}
			delete(wantFiles, filepath.Join("BepInEx", "plugins", "Owned-Earlier-1.9.0", "plugin.dll"))
			wantFiles[filepath.Join("BepInEx", "plugins", "Owned-Earlier-1.10.0", "plugin.dll")] = "replacement Earlier"
			paths, err := th.GetFileList(server)
			if err != nil {
				t.Fatal(err)
			}
			gotFiles := make(map[string]string)
			for relative, path := range paths {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				gotFiles[relative] = string(data)
			}
			if !reflect.DeepEqual(gotFiles, wantFiles) {
				t.Errorf("server files = %q, want %q", gotFiles, wantFiles)
			}
		})
	}
}
