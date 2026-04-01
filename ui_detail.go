package main

import (
	"encoding/base64"
	"io"
	"os/exec"
	"runtime"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// DetailPanel manages the profile edit form.
type DetailPanel struct {
	Container fyne.CanvasObject

	state  *AppState
	window fyne.Window

	iconImage        *fyne.Container
	nameEntry        *widget.Entry
	versionEntry     *widget.Entry
	typeSelect       *widget.Select
	gameDirEntry     *widget.Entry
	javaDirEntry     *widget.Entry
	javaArgsEntry    *widget.Entry
	resWEntry        *widget.Entry
	resHEntry        *widget.Entry
	modsFolderLbl    *widget.Label
	modsFolderRow    *fyne.Container
	modsStatusLbl    *canvas.Text
	checkModsBtn     *widget.Button
	searchModsBtn    *widget.Button
	shadersFolderLbl *widget.Label
	shadersFolderRow *fyne.Container
	checkShadersBtn  *widget.Button
	searchShadersBtn *widget.Button

	form     *widget.Form
	emptyMsg *fyne.Container
	stack    *fyne.Container

	updating bool // prevents OnChanged loops
}

// NewDetailPanel creates the detail editing panel.
func NewDetailPanel(state *AppState, win fyne.Window) *DetailPanel {
	dp := &DetailPanel{
		state:  state,
		window: win,
	}

	dp.iconImage = container.NewCenter()

	dp.nameEntry = widget.NewEntry()
	dp.nameEntry.OnChanged = func(s string) { dp.applyField(func(p *Profile) { p.Name = s }) }

	dp.versionEntry = widget.NewEntry()
	dp.versionEntry.PlaceHolder = "e.g., 1.21.1 or latest-release"
	dp.versionEntry.OnChanged = func(s string) { dp.applyField(func(p *Profile) { p.LastVersionId = s }) }

	dp.typeSelect = widget.NewSelect([]string{"custom", "latest-release", "latest-snapshot"}, func(s string) {
		dp.applyField(func(p *Profile) { p.Type = s })
	})

	dp.gameDirEntry = widget.NewEntry()
	dp.gameDirEntry.PlaceHolder = "Leave empty for default"
	dp.gameDirEntry.OnChanged = func(s string) { dp.applyField(func(p *Profile) { p.GameDir = s }) }

	gameDirBrowse := widget.NewButton("Browse...", func() {
		d := dialog.NewFolderOpen(func(lu fyne.ListableURI, err error) {
			if err != nil || lu == nil {
				return
			}
			dp.gameDirEntry.SetText(lu.Path())
		}, win)
		d.Show()
	})

	dp.javaDirEntry = widget.NewEntry()
	dp.javaDirEntry.PlaceHolder = "Leave empty for default"
	dp.javaDirEntry.OnChanged = func(s string) { dp.applyField(func(p *Profile) { p.JavaDir = s }) }

	dp.javaArgsEntry = widget.NewEntry()
	dp.javaArgsEntry.PlaceHolder = "e.g., -Xmx2G -Xms1G"
	dp.javaArgsEntry.OnChanged = func(s string) { dp.applyField(func(p *Profile) { p.JavaArgs = s }) }

	dp.resWEntry = widget.NewEntry()
	dp.resWEntry.PlaceHolder = "Width"
	dp.resWEntry.OnChanged = func(s string) { dp.applyResolution() }

	dp.resHEntry = widget.NewEntry()
	dp.resHEntry.PlaceHolder = "Height"
	dp.resHEntry.OnChanged = func(s string) { dp.applyResolution() }

	resRow := container.NewGridWithColumns(2, dp.resWEntry, dp.resHEntry)

	iconChangeBtn := widget.NewButton("Change Icon...", func() {
		dp.showIconPicker()
	})

	iconRow := container.NewHBox(dp.iconImage, iconChangeBtn)

	dp.modsFolderLbl = widget.NewLabel("")
	dp.modsFolderLbl.Truncation = fyne.TextTruncateEllipsis
	openModsBtn := widget.NewButton("Open", func() {
		path := dp.modsFolderLbl.Text
		if path == "" {
			return
		}
		openInFileManager(path)
	})
	dp.checkModsBtn = widget.NewButton("Check Mods", func() {
		path := dp.modsFolderLbl.Text
		if path == "" {
			return
		}
		prof := dp.state.Data.Profiles[dp.state.SelectedKey]
		if prof == nil {
			return
		}
		gameVersion := ResolveGameVersion(prof, dp.state.FilePath)
		showModsCheckDialog(path, gameVersion, dp.window)
	})
	dp.searchModsBtn = widget.NewButton("Search Mods", func() {
		path := dp.modsFolderLbl.Text
		if path == "" {
			return
		}
		prof := dp.state.Data.Profiles[dp.state.SelectedKey]
		if prof == nil {
			return
		}
		gameVersion := ResolveGameVersion(prof, dp.state.FilePath)
		loader := ResolveLoader(prof, dp.state.FilePath)
		showModSearchDialog(path, gameVersion, loader, dp.window)
	})
	modsButtons := container.NewHBox(openModsBtn, dp.checkModsBtn, dp.searchModsBtn)
	dp.modsStatusLbl = canvas.NewText("", theme.DisabledColor())
	dp.modsStatusLbl.TextSize = theme.TextSize() - 2
	dp.modsStatusLbl.Hide()
	dp.modsFolderRow = container.NewVBox(
		container.NewBorder(nil, nil, nil, modsButtons, dp.modsFolderLbl),
		dp.modsStatusLbl,
	)

	dp.shadersFolderLbl = widget.NewLabel("")
	dp.shadersFolderLbl.Truncation = fyne.TextTruncateEllipsis
	openShadersBtn := widget.NewButton("Open", func() {
		path := dp.shadersFolderLbl.Text
		if path == "" {
			return
		}
		openInFileManager(path)
	})
	dp.checkShadersBtn = widget.NewButton("Check Shaders", func() {
		path := dp.shadersFolderLbl.Text
		if path == "" {
			return
		}
		prof := dp.state.Data.Profiles[dp.state.SelectedKey]
		if prof == nil {
			return
		}
		gameVersion := ResolveGameVersion(prof, dp.state.FilePath)
		showShadersCheckDialog(path, gameVersion, dp.window)
	})
	dp.searchShadersBtn = widget.NewButton("Search Shaders", func() {
		path := dp.shadersFolderLbl.Text
		if path == "" {
			return
		}
		prof := dp.state.Data.Profiles[dp.state.SelectedKey]
		if prof == nil {
			return
		}
		gameVersion := ResolveGameVersion(prof, dp.state.FilePath)
		showShaderSearchDialog(path, gameVersion, dp.window)
	})
	shadersButtons := container.NewHBox(openShadersBtn, dp.checkShadersBtn, dp.searchShadersBtn)
	dp.shadersFolderRow = container.NewBorder(nil, nil, nil, shadersButtons, dp.shadersFolderLbl)

	dp.form = widget.NewForm(
		widget.NewFormItem("Icon", iconRow),
		widget.NewFormItem("Name", dp.nameEntry),
		widget.NewFormItem("Version", dp.versionEntry),
		widget.NewFormItem("Type", dp.typeSelect),
		widget.NewFormItem("Game Directory", container.NewBorder(nil, nil, nil, gameDirBrowse, dp.gameDirEntry)),
		widget.NewFormItem("Mods Folder", dp.modsFolderRow),
		widget.NewFormItem("Shaders Folder", dp.shadersFolderRow),
		widget.NewFormItem("Java Path", dp.javaDirEntry),
		widget.NewFormItem("Java Args", dp.javaArgsEntry),
		widget.NewFormItem("Resolution", resRow),
	)

	emptyLabel := widget.NewLabel("Select a profile to edit")
	emptyLabel.Alignment = fyne.TextAlignCenter
	dp.emptyMsg = container.NewCenter(emptyLabel)

	dp.stack = container.NewStack(dp.emptyMsg)
	dp.Container = dp.stack

	// Wire up selection callback
	state.OnSelect = func(key string) {
		dp.Refresh(key)
	}

	return dp
}

// Refresh populates the form with the given profile's data.
func (dp *DetailPanel) Refresh(key string) {
	prof := dp.state.Data.Profiles[key]
	if prof == nil {
		dp.stack.Objects = []fyne.CanvasObject{dp.emptyMsg}
		dp.stack.Refresh()
		return
	}

	dp.updating = true
	defer func() { dp.updating = false }()

	// Update icon display
	iconImg := ProfileIconImage(prof.Icon, 64)
	dp.iconImage.Objects = []fyne.CanvasObject{iconImg}
	dp.iconImage.Refresh()

	dp.nameEntry.SetText(prof.Name)
	dp.versionEntry.SetText(prof.LastVersionId)
	dp.typeSelect.SetSelected(prof.Type)
	dp.gameDirEntry.SetText(prof.GameDir)
	dp.javaDirEntry.SetText(prof.JavaDir)
	dp.javaArgsEntry.SetText(prof.JavaArgs)

	// Resolve and display mods and shaders folders
	loader := ""
	if dp.state.FilePath != "" {
		modsPath := ResolveModsFolder(prof, dp.state.FilePath)
		dp.modsFolderLbl.SetText(modsPath)
		shadersPath := ResolveShadersFolder(prof, dp.state.FilePath)
		dp.shadersFolderLbl.SetText(shadersPath)
		loader = ResolveLoader(prof, dp.state.FilePath)
	} else {
		dp.modsFolderLbl.SetText("")
		dp.shadersFolderLbl.SetText("")
	}

	if loader != "" {
		dp.checkModsBtn.Show()
		dp.searchModsBtn.Show()
		dp.modsStatusLbl.Text = ""
		dp.modsStatusLbl.Refresh()
		dp.modsStatusLbl.Hide()
	} else {
		dp.checkModsBtn.Hide()
		dp.searchModsBtn.Hide()
		dp.modsStatusLbl.Color = theme.DisabledColor()
		dp.modsStatusLbl.Text = "Mod actions are hidden because this profile does not have a detected mod loader."
		dp.modsStatusLbl.Refresh()
		dp.modsStatusLbl.Show()
	}
	dp.checkShadersBtn.Show()
	dp.searchShadersBtn.Show()
	dp.modsFolderRow.Refresh()
	dp.shadersFolderRow.Refresh()

	if prof.Resolution != nil {
		dp.resWEntry.SetText(strconv.Itoa(prof.Resolution.Width))
		dp.resHEntry.SetText(strconv.Itoa(prof.Resolution.Height))
	} else {
		dp.resWEntry.SetText("")
		dp.resHEntry.SetText("")
	}

	scrollable := container.NewVScroll(dp.form)
	dp.stack.Objects = []fyne.CanvasObject{scrollable}
	dp.stack.Refresh()
}

func (dp *DetailPanel) applyField(fn func(p *Profile)) {
	if dp.updating {
		return
	}
	prof := dp.state.Data.Profiles[dp.state.SelectedKey]
	if prof == nil {
		return
	}
	fn(prof)
	// Refresh list to show updated name
	if dp.state.OnListRefresh != nil {
		dp.state.OnListRefresh()
	}
}

func (dp *DetailPanel) applyResolution() {
	if dp.updating {
		return
	}
	prof := dp.state.Data.Profiles[dp.state.SelectedKey]
	if prof == nil {
		return
	}
	w, errW := strconv.Atoi(dp.resWEntry.Text)
	h, errH := strconv.Atoi(dp.resHEntry.Text)
	if errW != nil && errH != nil && dp.resWEntry.Text == "" && dp.resHEntry.Text == "" {
		prof.Resolution = nil
		return
	}
	if prof.Resolution == nil {
		prof.Resolution = &Resolution{}
	}
	if errW == nil {
		prof.Resolution.Width = w
	}
	if errH == nil {
		prof.Resolution.Height = h
	}
}

func (dp *DetailPanel) showIconPicker() {
	prof := dp.state.Data.Profiles[dp.state.SelectedKey]
	if prof == nil {
		return
	}

	// Offer named icons + custom file option
	namedIcons := []string{
		"Grass", "Dirt", "Stone", "Cobblestone", "Planks",
		"Iron", "Gold", "Diamond", "Lapis", "Emerald",
		"Redstone", "TNT", "Bookshelf", "Crafting_Table",
		"Furnace", "Brick", "Chest", "Pumpkin", "Bedrock",
		"Glass", "Creeper", "Pig", "Leather", "Log", "Cake",
		"Custom Image...",
	}

	list := widget.NewList(
		func() int { return len(namedIcons) },
		func() fyne.CanvasObject {
			return container.NewHBox(
				ProfileIconImage("Grass", 24),
				widget.NewLabel("Icon"),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			box := obj.(*fyne.Container)
			name := namedIcons[id]
			if name == "Custom Image..." {
				box.Objects[0] = layout.NewSpacer()
			} else {
				box.Objects[0] = ProfileIconImage(name, 24)
			}
			box.Objects[1].(*widget.Label).SetText(name)
			box.Refresh()
		},
	)

	d := dialog.NewCustom("Choose Icon", "Cancel", list, dp.window)
	d.Resize(fyne.NewSize(300, 400))

	list.OnSelected = func(id widget.ListItemID) {
		name := namedIcons[id]
		if name == "Custom Image..." {
			d.Hide()
			dp.pickCustomIcon()
			return
		}
		prof.Icon = name
		dp.Refresh(dp.state.SelectedKey)
		d.Hide()
	}

	d.Show()
}

func (dp *DetailPanel) pickCustomIcon() {
	fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil || rc == nil {
			return
		}
		defer rc.Close()

		prof := dp.state.Data.Profiles[dp.state.SelectedKey]
		if prof == nil {
			return
		}

		// Read and encode as base64 data URI
		importData, readErr := io.ReadAll(rc)
		if readErr != nil {
			return
		}

		encoded := base64.StdEncoding.EncodeToString(importData)
		prof.Icon = "data:image/png;base64," + encoded
		dp.Refresh(dp.state.SelectedKey)
	}, dp.window)
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".png", ".jpg", ".jpeg"}))
	fd.Show()
}

func openInFileManager(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("explorer", path)
	default: // linux
		cmd = exec.Command("xdg-open", path)
	}
	cmd.Start()
}
