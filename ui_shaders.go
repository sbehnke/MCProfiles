package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ShaderInfo holds local and remote info about an installed shader pack.
type ShaderInfo struct {
	Filename       string
	SHA1           string
	Path           string
	Found          bool
	ProjectID      string
	ProjectSlug    string
	ProjectTitle   string
	CurrentVersion string
	LatestVersion  string
	HasUpdate      bool
	UpdateURL      string
	UpdateFilename string
}

// versionPattern matches common version patterns in shader filenames.
var versionPattern = regexp.MustCompile(`[_\-\s][vrV]?(\d+\.\d+[\.\d]*)`)

// parseShaderFilename tries to extract a name and version from a shader zip filename.
func parseShaderFilename(filename string) (name, version string) {
	base := strings.TrimSuffix(filename, ".zip")
	base = strings.TrimSuffix(base, ".ZIP")

	loc := versionPattern.FindStringIndex(base)
	if loc == nil {
		return base, ""
	}

	name = base[:loc[0]]
	version = strings.TrimLeft(base[loc[0]:], "_- ")
	return name, version
}

// ScanShadersFolder finds all .zip files in the shaderpacks directory.
func ScanShadersFolder(dir string) (map[string]ScannedMod, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	hashes := make(map[string]ScannedMod)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".zip") {
			continue
		}
		fullPath := filepath.Join(dir, e.Name())
		h, err := hashJarFile(fullPath) // works for any file, not just jars
		if err != nil {
			continue
		}
		hashes[h] = ScannedMod{Filename: e.Name(), Path: fullPath}
	}
	return hashes, nil
}

// CheckShaders scans a shaderpacks folder and returns info about each shader.
func CheckShaders(shadersDir string, gameVersion string) ([]ShaderInfo, error) {
	hashToFile, err := ScanShadersFolder(shadersDir)
	if err != nil {
		return nil, fmt.Errorf("scanning shaderpacks folder: %w", err)
	}

	if len(hashToFile) == 0 {
		return nil, nil
	}

	hashes := make([]string, 0, len(hashToFile))
	for h := range hashToFile {
		hashes = append(hashes, h)
	}

	// Look up all hashes on Modrinth
	versions, err := LookupVersionsByHash(hashes)
	if err != nil {
		return nil, fmt.Errorf("looking up shaders: %w", err)
	}

	// Collect project IDs
	projectIDs := make(map[string]bool)
	for _, v := range versions {
		projectIDs[v.ProjectID] = true
	}

	// Check for updates
	foundHashes := make([]string, 0)
	for h := range versions {
		foundHashes = append(foundHashes, h)
	}

	var updates map[string]*ModrinthVersion
	if len(foundHashes) > 0 && gameVersion != "" {
		// Shaders work with any loader via Iris/Optifine, pass common ones
		updates, _ = CheckUpdates(foundHashes, []string{"iris", "optifine"}, []string{gameVersion})
		if updates == nil {
			// Try without loader filter
			updates, _ = CheckUpdates(foundHashes, []string{}, []string{gameVersion})
		}
	}

	ids := make([]string, 0, len(projectIDs))
	for id := range projectIDs {
		ids = append(ids, id)
	}
	projects, _ := LookupProjects(ids)

	var results []ShaderInfo
	for hash, scanned := range hashToFile {
		info := ShaderInfo{
			Filename: scanned.Filename,
			SHA1:     hash,
			Path:     scanned.Path,
		}

		parsedName, parsedVersion := parseShaderFilename(scanned.Filename)

		v, found := versions[hash]
		if !found {
			// Try slug-based lookup from filename
			slug := strings.ToLower(strings.ReplaceAll(parsedName, " ", "-"))
			if proj := LookupProjectBySlug(slug); proj != nil {
				info.Found = true
				info.ProjectID = proj.ID
				info.ProjectSlug = proj.Slug
				info.ProjectTitle = proj.Title
				info.CurrentVersion = parsedVersion

				slugVersions, err := GetProjectVersions(proj.ID, []string{}, []string{gameVersion})
				if err == nil && len(slugVersions) > 0 {
					latest := slugVersions[0]
					info.LatestVersion = latest.VersionNumber
					info.HasUpdate = latest.VersionNumber != parsedVersion
					if info.HasUpdate {
						info.UpdateURL, info.UpdateFilename = primaryFileURL(latest)
					}
				}
			} else {
				info.ProjectTitle = parsedName
				info.CurrentVersion = parsedVersion
			}

			results = append(results, info)
			continue
		}

		info.Found = true
		info.ProjectID = v.ProjectID
		info.CurrentVersion = v.VersionNumber

		if projects != nil {
			if p, ok := projects[v.ProjectID]; ok {
				info.ProjectTitle = p.Title
				info.ProjectSlug = p.Slug
			}
		}

		if updates != nil {
			if latest, ok := updates[hash]; ok {
				info.LatestVersion = latest.VersionNumber
				info.HasUpdate = latest.VersionNumber != v.VersionNumber
				if info.HasUpdate {
					info.UpdateURL, info.UpdateFilename = primaryFileURL(latest)
				}
			}
		}

		results = append(results, info)
	}

	return results, nil
}

// showShadersCheckDialog scans the shaderpacks folder and shows update status.
func showShadersCheckDialog(shadersDir string, gameVersion string, win fyne.Window) {
	progress := dialog.NewCustomWithoutButtons("Checking Shaders",
		widget.NewLabel("Scanning shader packs and checking Modrinth..."), win)
	progress.Resize(fyne.NewSize(350, 100))
	progress.Show()

	go func() {
		shaders, err := CheckShaders(shadersDir, gameVersion)
		if err != nil {
			fyne.Do(func() {
				progress.Hide()
				dialog.ShowError(fmt.Errorf("Error checking shaders: %w", err), win)
			})
			return
		}

		if len(shaders) == 0 {
			fyne.Do(func() {
				progress.Hide()
				dialog.ShowInformation("No Shaders", "No .zip files found in the shaderpacks folder.", win)
			})
			return
		}

		// Sort: updates first, then found, then unknown
		sort.Slice(shaders, func(i, j int) bool {
			if shaders[i].HasUpdate != shaders[j].HasUpdate {
				return shaders[i].HasUpdate
			}
			if shaders[i].Found != shaders[j].Found {
				return shaders[i].Found
			}
			return shaders[i].Filename < shaders[j].Filename
		})

		fyne.Do(func() {
			progress.Hide()
			showShadersResultDialog(shaders, win)
		})
	}()
}

func showShadersResultDialog(shaders []ShaderInfo, win fyne.Window) {
	summary := widget.NewLabel("")
	updateAllBtn := widget.NewButton("", nil)
	updateAllBtn.Importance = widget.HighImportance
	updateAllBtn.Hide()

	refreshSummary := func() {
		var foundCount, updateCount, unknownCount int
		for _, s := range shaders {
			switch {
			case !s.Found:
				unknownCount++
			case s.HasUpdate:
				updateCount++
			default:
				foundCount++
			}
		}
		summary.SetText(fmt.Sprintf("%d shaders: %d up to date, %d updates available, %d not on Modrinth",
			len(shaders), foundCount, updateCount, unknownCount))
		if updateCount > 0 {
			updateAllBtn.SetText(fmt.Sprintf("Update All (%d)", updateCount))
			updateAllBtn.Show()
		} else {
			updateAllBtn.Hide()
		}
	}

	var shaderList *widget.List
	shaderList = widget.NewList(
		func() int { return len(shaders) },
		func() fyne.CanvasObject {
			icon := widget.NewIcon(theme.ConfirmIcon())
			name := widget.NewLabel("Shader Name")
			name.TextStyle = fyne.TextStyle{Bold: true}
			version := widget.NewLabel("version")
			updateBtn := widget.NewButton("Update", nil)
			updateBtn.Importance = widget.HighImportance
			webBtn := widget.NewButtonWithIcon("", theme.ComputerIcon(), nil)
			uninstallBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), nil)
			uninstallBtn.Importance = widget.DangerImportance
			buttons := container.NewHBox(updateBtn, webBtn, uninstallBtn)
			return container.NewBorder(nil, nil,
				container.NewHBox(icon, name), buttons, version)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			row := obj.(*fyne.Container)
			version := row.Objects[0].(*widget.Label)
			left := row.Objects[1].(*fyne.Container)
			buttons := row.Objects[2].(*fyne.Container)
			icon := left.Objects[0].(*widget.Icon)
			name := left.Objects[1].(*widget.Label)
			updateBtn := buttons.Objects[0].(*widget.Button)
			webBtn := buttons.Objects[1].(*widget.Button)
			uninstallBtn := buttons.Objects[2].(*widget.Button)

			s := shaders[id]
			displayName := s.ProjectTitle
			if displayName == "" {
				displayName = s.Filename
			}
			name.SetText(displayName)

			// Uninstall
			uninstallBtn.OnTapped = func() {
				dialog.ShowConfirm("Uninstall Shader",
					fmt.Sprintf("Remove %s?", displayName),
					func(ok bool) {
						if !ok {
							return
						}
						if err := os.Remove(s.Path); err != nil {
							dialog.ShowError(err, win)
							return
						}
						shaders = append(shaders[:id], shaders[id+1:]...)
						shaderList.Refresh()
						refreshSummary()
					}, win)
			}

			// Web
			if s.Found && s.ProjectSlug != "" {
				webBtn.Show()
				slug := s.ProjectSlug
				webBtn.OnTapped = func() {
					u, _ := url.Parse("https://modrinth.com/shader/" + slug)
					fyne.CurrentApp().OpenURL(u)
				}
			} else {
				webBtn.Hide()
			}

			if !s.Found {
				icon.SetResource(theme.QuestionIcon())
				if s.CurrentVersion != "" {
					version.SetText(s.CurrentVersion + " (not on Modrinth)")
				} else {
					version.SetText("Not on Modrinth")
				}
				updateBtn.Hide()
			} else if s.HasUpdate {
				icon.SetResource(theme.UploadIcon())
				version.SetText(fmt.Sprintf("%s \u2192 %s", s.CurrentVersion, s.LatestVersion))
				updateBtn.Show()
				updateBtn.SetText("Update")
				updateBtn.Enable()
				updateBtn.OnTapped = func() {
					updateBtn.Disable()
					updateBtn.SetText("...")
					go func() {
						_, dlErr := downloadAndReplace(s.UpdateURL, s.UpdateFilename, s.Path)
						fyne.Do(func() {
							if dlErr != nil {
								updateBtn.SetText("Retry")
								updateBtn.Enable()
								dialog.ShowError(fmt.Errorf("Failed to update %s: %w", displayName, dlErr), win)
								return
							}
							shaders[id].HasUpdate = false
							icon.SetResource(theme.ConfirmIcon())
							version.SetText(s.LatestVersion)
							updateBtn.Hide()
							refreshSummary()
						})
					}()
				}
			} else {
				icon.SetResource(theme.ConfirmIcon())
				version.SetText(s.CurrentVersion)
				updateBtn.Hide()
			}
		},
	)

	// Wire up Update All
	updateAllBtn.OnTapped = func() {
		updateAllBtn.Disable()
		updateAllBtn.SetText("Updating...")
		go func() {
			var failed int
			for i := range shaders {
				if !shaders[i].HasUpdate || shaders[i].UpdateURL == "" {
					continue
				}
				_, err := downloadAndReplace(shaders[i].UpdateURL, shaders[i].UpdateFilename, shaders[i].Path)
				if err != nil {
					failed++
					continue
				}
				shaders[i].HasUpdate = false
			}
			fyne.Do(func() {
				shaderList.Refresh()
				refreshSummary()
				if failed > 0 {
					updateAllBtn.SetText(fmt.Sprintf("%d failed", failed))
					updateAllBtn.Show()
				}
			})
		}()
	}

	refreshSummary()
	topRow := container.NewBorder(nil, nil, summary, updateAllBtn)
	content := container.NewBorder(topRow, nil, nil, nil, shaderList)

	d := dialog.NewCustom("Shader Status", "Close", content, win)
	d.Resize(fyne.NewSize(600, 400))
	d.Show()
}

// showShaderSearchDialog shows a dialog to search for and install shaders from Modrinth.
func showShaderSearchDialog(shadersDir string, gameVersion string, win fyne.Window) {
	installedMap := make(map[string]string) // projectID -> path

	var results []ModrinthSearchResult
	resultsBox := container.NewVBox()
	resultsScroll := container.NewVScroll(resultsBox)

	statusLabel := widget.NewLabel("Search for shader packs on Modrinth")
	var refreshResults func()
	refreshInstalledMap := func() {
		go func() {
			m := InstalledShaderMap(shadersDir)
			fyne.Do(func() {
				installedMap = m
				if refreshResults != nil {
					refreshResults()
				}
			})
		}()
	}

	refreshResults = func() {
		rows := make([]fyne.CanvasObject, 0, len(results))
		for _, r := range results {
			title := widget.NewLabel("Shader Title")
			title.TextStyle = fyne.TextStyle{Bold: true}
			title.Truncation = fyne.TextTruncateEllipsis
			title.Wrapping = fyne.TextWrapOff
			downloads := widget.NewLabel("0 downloads")
			downloads.TextStyle = fyne.TextStyle{Italic: true}
			downloads.Wrapping = fyne.TextWrapOff
			titleRow := container.NewBorder(nil, nil, nil, downloads, title)
			desc := widget.NewLabel("Description")
			desc.Truncation = fyne.TextTruncateEllipsis
			actionBtn := widget.NewButtonWithIcon("Install", theme.DownloadIcon(), nil)
			actionBtn.Importance = widget.HighImportance
			webBtn := widget.NewButtonWithIcon("Web", theme.ComputerIcon(), nil)
			buttons := container.NewHBox(actionBtn, webBtn)
			info := container.NewVBox(titleRow, desc)
			title.SetText(r.Title)
			desc.SetText(r.Description)
			downloads.SetText(formatDownloads(r.Downloads))

			if path, installed := installedMap[r.ProjectID]; installed {
				// Already installed — show Uninstall
				actionBtn.SetText("Uninstall")
				actionBtn.SetIcon(theme.DeleteIcon())
				actionBtn.Importance = widget.DangerImportance
				actionBtn.Enable()
				actionBtn.OnTapped = func() {
					dialog.ShowConfirm("Uninstall Shader",
						fmt.Sprintf("Remove %s?", r.Title),
						func(ok bool) {
							if !ok {
								return
							}
							if err := os.Remove(path); err != nil {
								dialog.ShowError(err, win)
								return
							}
							refreshInstalledMap()
						}, win)
				}
			} else {
				// Not installed — show Install
				actionBtn.SetText("Install")
				actionBtn.SetIcon(theme.DownloadIcon())
				actionBtn.Importance = widget.HighImportance
				actionBtn.Enable()
				actionBtn.OnTapped = func() {
					actionBtn.Disable()
					actionBtn.SetText("...")

					gameVersions := []string{}
					if gameVersion != "" {
						gameVersions = []string{gameVersion}
					}

					go func() {
						versions, err := GetProjectVersions(r.ProjectID, []string{}, gameVersions)
						if err != nil || len(versions) == 0 {
							fyne.Do(func() {
								actionBtn.SetText("Failed")
								actionBtn.Enable()
								errMsg := "no compatible version found"
								if err != nil {
									errMsg = err.Error()
								}
								dialog.ShowError(fmt.Errorf("Failed to install %s: %s", r.Title, errMsg), win)
							})
							return
						}

						dlURL, filename := primaryFileURL(versions[0])
						_, dlErr := downloadToDir(dlURL, filename, shadersDir)
						fyne.Do(func() {
							if dlErr != nil {
								actionBtn.SetText("Failed")
								actionBtn.Enable()
								dialog.ShowError(fmt.Errorf("Failed to install %s: %w", r.Title, dlErr), win)
								return
							}
							refreshInstalledMap()
							dialog.ShowInformation("Installed", fmt.Sprintf("Installed: %s", filename), win)
						})
					}()
				}
			}

			webBtn.OnTapped = func() {
				slug := r.Slug
				if slug == "" {
					slug = r.ProjectID
				}
				u, _ := url.Parse("https://modrinth.com/shader/" + slug)
				fyne.CurrentApp().OpenURL(u)
			}

			rows = append(rows, container.NewBorder(nil, nil, nil, buttons, info))
		}
		resultsBox.Objects = rows
		resultsBox.Refresh()
		resultsScroll.ScrollToTop()
	}

	searchEntry := widget.NewEntry()
	searchEntry.PlaceHolder = "Search shaders..."

	searchBtn := widget.NewButtonWithIcon("Search", theme.SearchIcon(), nil)
	doSearch := func() {
		query := searchEntry.Text
		if strings.TrimSpace(query) == "" {
			return
		}
		searchBtn.Disable()
		statusLabel.SetText("Searching...")

		go func() {
			resp, err := SearchShaders(query, gameVersion)
			fyne.Do(func() {
				searchBtn.Enable()
				if err != nil {
					statusLabel.SetText("Search failed")
					dialog.ShowError(err, win)
					return
				}
				results = resp.Hits
				refreshResults()
				statusLabel.SetText(fmt.Sprintf("%d results", len(results)))
			})
		}()
	}

	searchBtn.OnTapped = doSearch
	searchEntry.OnSubmitted = func(_ string) { doSearch() }

	searchRow := container.NewBorder(nil, nil, nil, searchBtn, searchEntry)
	top := container.NewVBox(searchRow, statusLabel)
	content := container.NewBorder(top, nil, nil, nil, resultsScroll)

	d := dialog.NewCustom("Search Shaders — Modrinth", "Close", content, win)
	d.Resize(fyne.NewSize(650, 500))
	d.Show()
	refreshInstalledMap()
}

// downloadAndReplace downloads a file and removes the old one.
func downloadAndReplace(dlURL, newFilename, oldPath string) (string, error) {
	dir := filepath.Dir(oldPath)
	newPath, err := downloadToDir(dlURL, newFilename, dir)
	if err != nil {
		return "", err
	}
	// Remove old file (only if different name)
	if newPath != oldPath {
		os.Remove(oldPath)
	}
	return newPath, nil
}
