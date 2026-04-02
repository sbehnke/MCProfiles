package main

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func showVersionPickerDialog(title string, projectTitle string, projectID string, currentVersion string, loaders []string, gameVersions []string, actionLabel string, win fyne.Window, onSelect func(selected *ModrinthVersion, latest *ModrinthVersion)) {
	progress := dialog.NewCustomWithoutButtons(title, widget.NewLabel("Loading compatible versions..."), win)
	progress.Resize(fyne.NewSize(420, 120))
	progress.Show()

	go func() {
		versions, err := ListCompatibleVersions(projectID, loaders, gameVersions)
		fyne.Do(func() {
			progress.Hide()
			if err != nil {
				dialog.ShowError(fmt.Errorf("Failed to load versions for %s: %w", projectTitle, err), win)
				return
			}
			if len(versions) == 0 {
				dialog.ShowInformation(title, "No compatible stable versions found.", win)
				return
			}

			latest := pickBestVersion(versions, firstGameVersion(gameVersions))
			rows := make([]fyne.CanvasObject, 0, len(versions))
			var picker dialog.Dialog

			for _, version := range versions {
				versionName := version.VersionNumber
				if versionName == "" {
					versionName = version.Name
				}
				if versionName == "" {
					versionName = version.ID
				}

				header := widget.NewLabel(versionName)
				header.TextStyle = fyne.TextStyle{Bold: true}
				header.Truncation = fyne.TextTruncateEllipsis
				header.Wrapping = fyne.TextWrapOff

				meta := widget.NewLabel(formatVersionPickerMeta(version))
				meta.Wrapping = fyne.TextWrapWord

				noteText := ""
				if currentVersion != "" && version.VersionNumber == currentVersion {
					noteText = "Current installed version"
				} else if latest != nil && version.ID == latest.ID {
					noteText = "Recommended stable version"
				}

				note := widget.NewLabel(noteText)
				if noteText == "" {
					note.Hide()
				} else {
					note.TextStyle = fyne.TextStyle{Italic: true}
				}

				selectBtn := widget.NewButton(actionLabel, nil)
				selectBtn.Importance = widget.HighImportance
				selectedVersion := version
				selectBtn.OnTapped = func() {
					if picker != nil {
						picker.Hide()
					}
					onSelect(selectedVersion, latest)
				}

				info := container.NewVBox(header, meta, note)
				rows = append(rows, container.NewBorder(nil, nil, widget.NewIcon(theme.DocumentIcon()), selectBtn, info))
			}

			content := container.NewBorder(
				widget.NewLabel(fmt.Sprintf("%d stable compatible versions", len(versions))),
				nil, nil, nil,
				container.NewVScroll(container.NewVBox(rows...)),
			)
			picker = dialog.NewCustom(title, "Close", content, win)
			picker.Resize(fyne.NewSize(720, 480))
			picker.Show()
		})
	}()
}

func formatVersionPickerMeta(version *ModrinthVersion) string {
	parts := []string{}
	if version.DatePublished.IsZero() {
		parts = append(parts, "Published: unknown")
	} else {
		parts = append(parts, "Published: "+version.DatePublished.Local().Format("2006-01-02"))
	}

	versionType := version.VersionType
	if versionType == "" {
		versionType = "release"
	}
	parts = append(parts, "Channel: "+capitalize(versionType))

	if len(version.GameVersions) > 0 {
		parts = append(parts, "Minecraft: "+strings.Join(version.GameVersions, ", "))
	}
	if len(version.Loaders) > 0 {
		parts = append(parts, "Loaders: "+strings.Join(version.Loaders, ", "))
	}

	return strings.Join(parts, "\n")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
