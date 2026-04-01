package main

import (
	"fmt"
	"net/url"
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// showModsCheckDialog scans the mods folder and shows update/dependency status.
func showModsCheckDialog(modsDir string, gameVersion string, win fyne.Window) {
	// Show a progress dialog while scanning
	progress := dialog.NewCustomWithoutButtons("Checking Mods",
		widget.NewLabel("Scanning mods and checking Modrinth..."), win)
	progress.Resize(fyne.NewSize(350, 100))
	progress.Show()

	go func() {
		mods, err := CheckMods(modsDir, gameVersion)
		if err != nil {
			fyne.Do(func() {
				progress.Hide()
				dialog.ShowError(fmt.Errorf("Error checking mods: %w", err), win)
			})
			return
		}

		if len(mods) == 0 {
			fyne.Do(func() {
				progress.Hide()
				dialog.ShowInformation("No Mods", "No .jar files found in the mods folder.", win)
			})
			return
		}

		// Collect projects for dependency name lookup
		projectIDs := make(map[string]bool)
		for _, m := range mods {
			if m.Found {
				projectIDs[m.ProjectID] = true
				for _, dep := range m.Dependencies {
					if dep.ProjectID != "" {
						projectIDs[dep.ProjectID] = true
					}
				}
			}
		}
		ids := make([]string, 0, len(projectIDs))
		for id := range projectIDs {
			ids = append(ids, id)
		}
		projects, _ := LookupProjects(ids)

		missingDeps := FindMissingDependencies(mods, projects)

		// Sort mods: updates first, then found, then unknown
		sort.Slice(mods, func(i, j int) bool {
			if mods[i].HasUpdate != mods[j].HasUpdate {
				return mods[i].HasUpdate
			}
			if mods[i].Found != mods[j].Found {
				return mods[i].Found
			}
			return mods[i].Filename < mods[j].Filename
		})

		fyne.Do(func() {
			progress.Hide()
			showModsResultDialog(mods, missingDeps, projects, win)
		})
	}()
}

// showModsResultDialog displays the results of a mod check.
func showModsResultDialog(mods []ModInfo, missingDeps []MissingDep, projects map[string]*ModrinthProject, win fyne.Window) {
	// Summary and Update All button — declared early so update callbacks can refresh them
	summary := widget.NewLabel("")
	updateAllBtn := widget.NewButton("", nil)
	updateAllBtn.Importance = widget.HighImportance
	updateAllBtn.Hide()

	refreshSummary := func() {
		var foundCount, updateCount, unknownCount int
		for _, m := range mods {
			switch {
			case !m.Found:
				unknownCount++
			case m.HasUpdate:
				updateCount++
			default:
				foundCount++
			}
		}
		summary.SetText(fmt.Sprintf("%d mods: %d up to date, %d updates available, %d not on Modrinth",
			len(mods), foundCount, updateCount, unknownCount))
		if updateCount > 0 {
			updateAllBtn.SetText(fmt.Sprintf("Update All (%d)", updateCount))
			updateAllBtn.Show()
		} else {
			updateAllBtn.Hide()
		}
	}

	// Build the mod list
	var modList *widget.List
	modList = widget.NewList(
		func() int { return len(mods) },
		func() fyne.CanvasObject {
			icon := widget.NewIcon(theme.ConfirmIcon())
			name := widget.NewLabel("Mod Name That Is Long Enough")
			name.TextStyle = fyne.TextStyle{Bold: true}
			version := widget.NewLabel("version info here")
			version.Wrapping = fyne.TextWrapOff
			updateBtn := widget.NewButton("Update", nil)
			updateBtn.Importance = widget.HighImportance
			webBtn := widget.NewButtonWithIcon("", theme.ComputerIcon(), nil)
			uninstallBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), nil)
			uninstallBtn.Importance = widget.DangerImportance
			buttons := container.NewHBox(updateBtn, webBtn, uninstallBtn)
			row := container.NewBorder(nil, nil,
				container.NewHBox(icon, name), buttons, version)
			return row
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			row := obj.(*fyne.Container)
			// NewBorder orders: [center..., left, right]
			version := row.Objects[0].(*widget.Label)
			left := row.Objects[1].(*fyne.Container)
			buttons := row.Objects[2].(*fyne.Container)
			icon := left.Objects[0].(*widget.Icon)
			name := left.Objects[1].(*widget.Label)
			updateBtn := buttons.Objects[0].(*widget.Button)
			webBtn := buttons.Objects[1].(*widget.Button)
			uninstallBtn := buttons.Objects[2].(*widget.Button)

			m := mods[id]

			displayName := m.ProjectTitle
			if displayName == "" {
				displayName = m.Filename
			}
			name.SetText(displayName)

			// Uninstall button — always available
			uninstallBtn.OnTapped = func() {
				dialog.ShowConfirm("Uninstall Mod",
					fmt.Sprintf("Remove %s?", displayName),
					func(ok bool) {
						if !ok {
							return
						}
						if err := UninstallMod(m.InstalledPath); err != nil {
							dialog.ShowError(err, win)
							return
						}
						// Remove from list
						mods = append(mods[:id], mods[id+1:]...)
						modList.Refresh()
					}, win)
			}

			// Web button — only for mods found on Modrinth
			if m.Found {
				webBtn.Show()
				slug := m.ProjectID
				if projects != nil {
					if p, ok := projects[m.ProjectID]; ok && p.Slug != "" {
						slug = p.Slug
					}
				}
				webBtn.OnTapped = func() {
					u, _ := url.Parse(ProjectURL(slug))
					fyne.CurrentApp().OpenURL(u)
				}
			} else {
				webBtn.Hide()
			}

			if !m.Found {
				icon.SetResource(theme.QuestionIcon())
				if m.CurrentVersion != "" {
					version.SetText(m.CurrentVersion + " (not on Modrinth)")
				} else {
					version.SetText("Not on Modrinth")
				}
				updateBtn.Hide()
			} else if m.HasUpdate {
				icon.SetResource(theme.UploadIcon())
				version.SetText(fmt.Sprintf("%s \u2192 %s", m.CurrentVersion, m.LatestVersion))
				updateBtn.Show()
				updateBtn.SetText("Update")
				updateBtn.Enable()
				updateBtn.OnTapped = func() {
					updateBtn.Disable()
					updateBtn.SetText("...")
					go func() {
						_, dlErr := DownloadMod(m)
						fyne.Do(func() {
							if dlErr != nil {
								updateBtn.SetText("Retry")
								updateBtn.Enable()
								dialog.ShowError(fmt.Errorf("Failed to update %s: %w", displayName, dlErr), win)
								return
							}
							mods[id].HasUpdate = false
							icon.SetResource(theme.ConfirmIcon())
							version.SetText(m.LatestVersion)
							updateBtn.Hide()
							refreshSummary()
						})
					}()
				}
			} else {
				icon.SetResource(theme.ConfirmIcon())
				version.SetText(m.CurrentVersion)
				updateBtn.Hide()
			}
		},
	)

	// Wire up Update All button
	updateAllBtn.OnTapped = func() {
		updateAllBtn.Disable()
		updateAllBtn.SetText("Updating...")
		go func() {
			var failed int
			for i := range mods {
				if !mods[i].HasUpdate || mods[i].UpdateURL == "" {
					continue
				}
				_, err := DownloadMod(mods[i])
				if err != nil {
					failed++
					continue
				}
				mods[i].HasUpdate = false
			}
			fyne.Do(func() {
				modList.Refresh()
				refreshSummary()
				if failed > 0 {
					updateAllBtn.SetText(fmt.Sprintf("%d failed", failed))
					updateAllBtn.Show()
				}
			})
		}()
	}

	// Initial summary
	refreshSummary()
	topRow := container.NewBorder(nil, nil, summary, updateAllBtn)

	content := container.NewBorder(topRow, nil, nil, nil, modList)

	// If there are missing dependencies, add a section
	if len(missingDeps) > 0 {
		depHeader := widget.NewLabel("Missing Required Dependencies:")
		depHeader.TextStyle = fyne.TextStyle{Bold: true}

		depList := widget.NewList(
			func() int { return len(missingDeps) },
			func() fyne.CanvasObject {
				icon := widget.NewIcon(theme.WarningIcon())
				name := widget.NewLabel("Dependency")
				info := widget.NewLabel("required by")
				return container.NewHBox(icon, name, info)
			},
			func(id widget.ListItemID, obj fyne.CanvasObject) {
				row := obj.(*fyne.Container)
				name := row.Objects[1].(*widget.Label)
				info := row.Objects[2].(*widget.Label)

				dep := missingDeps[id]
				name.SetText(dep.ProjectTitle)
				info.SetText(fmt.Sprintf("(required by %s)", dep.RequiredBy))
			},
		)

		depSection := container.NewBorder(depHeader, nil, nil, nil, depList)

		// Split between mods and deps
		split := container.NewVSplit(content, depSection)
		split.SetOffset(0.7)
		content = container.NewStack(split)
	}

	d := dialog.NewCustom("Mod Status", "Close", content, win)
	d.Resize(fyne.NewSize(600, 500))
	d.Show()
}
