//go:build !headless

package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// profileDisplayName returns name with version fallback.
func profileDisplayName(p *Profile) string {
	if p.Name != "" {
		return p.Name
	}
	if p.LastVersionId != "" {
		return p.LastVersionId
	}
	return "(unnamed)"
}

// NewProfileList creates the sidebar profile list.
func NewProfileList(state *AppState) *widget.List {
	list := widget.NewList(
		func() int {
			return len(state.SortedKeys)
		},
		func() fyne.CanvasObject {
			icon := ProfileIconImage("Grass", 32)
			iconBox := container.NewCenter(icon)
			iconBox.Resize(fyne.NewSize(36, 36))
			label := widget.NewLabel("Profile Name")
			label.Truncation = fyne.TextTruncateEllipsis
			return container.NewBorder(nil, nil, iconBox, nil, label)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(state.SortedKeys) {
				return
			}
			key := state.SortedKeys[id]
			prof := state.Data.Profiles[key]
			if prof == nil {
				return
			}

			box := obj.(*fyne.Container)

			// Objects[0] = center (label), Objects[1] = left (iconBox)
			iconBox := box.Objects[1].(*fyne.Container)
			newIcon := ProfileIconImage(prof.Icon, 32)
			iconBox.Objects = []fyne.CanvasObject{newIcon}
			iconBox.Refresh()

			lbl := box.Objects[0].(*widget.Label)
			lbl.SetText(profileDisplayName(prof))
		},
	)

	list.OnSelected = func(id widget.ListItemID) {
		if id < len(state.SortedKeys) {
			state.SelectedKey = state.SortedKeys[id]
			if state.OnSelect != nil {
				state.OnSelect(state.SelectedKey)
			}
		}
	}

	return list
}
