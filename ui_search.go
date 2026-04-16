//go:build !headless

package main

import (
	"fmt"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// showModSearchDialog shows a dialog to search for and install mods from Modrinth.
func showModSearchDialog(modsDir string, gameVersion string, loader string, win fyne.Window) {
	installedMap := make(map[string]string) // projectID -> path

	var results []ModrinthSearchResult
	resultsBox := container.NewVBox()
	resultsScroll := container.NewVScroll(resultsBox)

	statusLabel := widget.NewLabel("Search for mods on Modrinth")
	var refreshResults func()
	refreshInstalledMap := func() {
		go func() {
			m := InstalledModMap(modsDir)
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
			title := widget.NewLabel("Mod Title")
			title.TextStyle = fyne.TextStyle{Bold: true}
			title.Truncation = fyne.TextTruncateEllipsis
			title.Wrapping = fyne.TextWrapOff
			downloads := widget.NewLabel("0 downloads")
			downloads.TextStyle = fyne.TextStyle{Italic: true}
			downloads.Wrapping = fyne.TextWrapOff
			titleRow := container.NewBorder(nil, nil, nil, downloads, title)
			desc := widget.NewLabel("Description text here")
			desc.Truncation = fyne.TextTruncateEllipsis
			actionBtn := widget.NewButtonWithIcon("Install", theme.DownloadIcon(), nil)
			actionBtn.Importance = widget.HighImportance
			advancedBtn := widget.NewButton("Advanced...", nil)
			webBtn := widget.NewButtonWithIcon("Web", theme.ComputerIcon(), nil)
			buttons := container.NewHBox(actionBtn, advancedBtn, webBtn)
			info := container.NewVBox(titleRow, desc)
			title.SetText(r.Title)
			desc.SetText(r.Description)
			downloads.SetText(formatDownloads(r.Downloads))

			if path, installed := installedMap[r.ProjectID]; installed {
				// Already installed — show Uninstall
				actionBtn.SetText("Uninstall")
				actionBtn.SetIcon(theme.DeleteIcon())
				actionBtn.Importance = widget.DangerImportance
				advancedBtn.Hide()
				actionBtn.Enable()
				actionBtn.OnTapped = func() {
					dialog.ShowConfirm("Uninstall Mod",
						fmt.Sprintf("Remove %s?", r.Title),
						func(ok bool) {
							if !ok {
								return
							}
							if err := UninstallMod(path); err != nil {
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
				advancedBtn.Show()
				actionBtn.Enable()
				setBusy := func(busy bool) {
					if busy {
						actionBtn.Disable()
						advancedBtn.Disable()
						actionBtn.SetText("...")
						return
					}
					actionBtn.Enable()
					advancedBtn.Enable()
					actionBtn.SetText("Install")
				}
				modLoaders := r.Loaders()
				gameVersions := []string{}
				if gameVersion != "" {
					gameVersions = []string{gameVersion}
				}

				chooseLoader := func(confirmText string, onSelected func(string)) {
					if loader != "" {
						for _, ml := range modLoaders {
							if ml == loader {
								onSelected(loader)
								return
							}
						}
					}

					if len(modLoaders) <= 1 {
						selected := ""
						if len(modLoaders) == 1 {
							selected = modLoaders[0]
						}
						onSelected(selected)
						return
					}

					loaderSelect := widget.NewSelect(modLoaders, nil)
					if loader != "" {
						loaderSelect.SetSelected(loader)
					} else {
						loaderSelect.SetSelected(modLoaders[0])
					}
					d := dialog.NewForm("Select Loader",
						confirmText, "Cancel",
						[]*widget.FormItem{widget.NewFormItem("Loader", loaderSelect)},
						func(ok bool) {
							if ok && loaderSelect.Selected != "" {
								onSelected(loaderSelect.Selected)
							}
						}, win)
					d.Show()
				}

				actionBtn.OnTapped = func() {
					chooseLoader("Install", func(selectedLoader string) {
						setBusy(true)
						loaders := []string{}
						if selectedLoader != "" {
							loaders = []string{selectedLoader}
						}
						go func() {
							installed, err := InstallModWithDeps(r.ProjectID, modsDir, loaders, gameVersions)
							fyne.Do(func() {
								if err != nil {
									setBusy(false)
									dialog.ShowError(fmt.Errorf("Failed to install %s: %w", r.Title, err), win)
									return
								}
								refreshInstalledMap()
								dialog.ShowInformation("Installed", fmt.Sprintf("Installed: %s", strings.Join(installed, ", ")), win)
							})
						}()
					})
				}

				advancedBtn.OnTapped = func() {
					chooseLoader("Choose", func(selectedLoader string) {
						loaders := []string{}
						if selectedLoader != "" {
							loaders = []string{selectedLoader}
						}
						showVersionPickerDialog(
							"Select Mod Version",
							r.Title,
							r.ProjectID,
							"",
							loaders,
							gameVersions,
							"Install",
							win,
							func(selected *ModrinthVersion, _ *ModrinthVersion) {
								setBusy(true)
								go func() {
									installed, err := InstallSpecificVersionWithDeps(selected, modsDir, loaders, gameVersions)
									fyne.Do(func() {
										if err != nil {
											setBusy(false)
											dialog.ShowError(fmt.Errorf("Failed to install %s: %w", r.Title, err), win)
											return
										}
										refreshInstalledMap()
										dialog.ShowInformation("Installed", fmt.Sprintf("Installed: %s", strings.Join(installed, ", ")), win)
									})
								}()
							},
						)
					})
				}
			}

			webBtn.OnTapped = func() {
				slug := r.Slug
				if slug == "" {
					slug = r.ProjectID
				}
				u, _ := url.Parse(ProjectURL(slug))
				fyne.CurrentApp().OpenURL(u)
			}

			rows = append(rows, container.NewBorder(nil, nil, nil, buttons, info))
		}
		resultsBox.Objects = rows
		resultsBox.Refresh()
		resultsScroll.ScrollToTop()
	}

	searchEntry := widget.NewEntry()
	searchEntry.PlaceHolder = "Search mods..."

	searchBtn := widget.NewButtonWithIcon("Search", theme.SearchIcon(), nil)
	doSearch := func() {
		query := searchEntry.Text
		if strings.TrimSpace(query) == "" {
			return
		}
		searchBtn.Disable()
		statusLabel.SetText("Searching...")

		go func() {
			resp, err := SearchMods(query, gameVersion, loader)
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

	d := dialog.NewCustom("Search Mods — Modrinth", "Close", content, win)
	d.Resize(fyne.NewSize(650, 500))
	d.Show()
	refreshInstalledMap()
}

// formatDownloads formats a download count for display.
func formatDownloads(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM downloads", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK downloads", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d downloads", n)
	}
}
