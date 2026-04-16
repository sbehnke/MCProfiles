//go:build !headless

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const prefLastFile = "lastProfilePath"

// AppState holds the shared application state.
type AppState struct {
	Data          *LauncherData
	FilePath      string
	SelectedKey   string
	SortedKeys    []string
	OnSelect      func(key string)
	OnListRefresh func()
}

// RefreshKeys rebuilds the sorted key list from Data.Profiles.
func (s *AppState) RefreshKeys() {
	s.SortedKeys = make([]string, 0, len(s.Data.Profiles))
	for k := range s.Data.Profiles {
		s.SortedKeys = append(s.SortedKeys, k)
	}
	sort.Slice(s.SortedKeys, func(i, j int) bool {
		pi := s.Data.Profiles[s.SortedKeys[i]]
		pj := s.Data.Profiles[s.SortedKeys[j]]
		// Pin latest-release and latest-snapshot to top
		ti := profileTypePriority(pi.Type)
		tj := profileTypePriority(pj.Type)
		if ti != tj {
			return ti < tj
		}
		return strings.ToLower(pi.Name) < strings.ToLower(pj.Name)
	})
}

func profileTypePriority(t string) int {
	switch t {
	case "latest-release":
		return 0
	case "latest-snapshot":
		return 1
	default:
		return 2
	}
}

func main() {
	a := app.NewWithID("dev.moat.mcprofiles")
	w := a.NewWindow("MC Profile Editor")
	w.Resize(fyne.NewSize(900, 600))

	state := &AppState{}
	pathLabel := widget.NewLabel("")
	pathLabel.Truncation = fyne.TextTruncateEllipsis

	var profileList *widget.List
	var detailPanel *DetailPanel

	buildUI := func() {
		state.RefreshKeys()

		profileList = NewProfileList(state)
		detailPanel = NewDetailPanel(state, w)

		state.OnListRefresh = func() {
			state.RefreshKeys()
			profileList.Refresh()
		}

		// Toolbar
		toolbar := widget.NewToolbar(
			widget.NewToolbarAction(theme.FolderOpenIcon(), func() {
				fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
					if err != nil || rc == nil {
						return
					}
					rc.Close()
					loadFile(state, rc.URI().Path(), w, profileList, detailPanel, pathLabel, a)
				}, w)
				fd.Show()
			}),
			widget.NewToolbarAction(theme.DocumentSaveIcon(), func() {
				if state.FilePath == "" {
					return
				}
				if err := SaveProfiles(state.FilePath, state.Data); err != nil {
					dialog.ShowError(err, w)
					return
				}
				dialog.ShowInformation("Saved", "Profiles saved successfully.", w)
			}),
			widget.NewToolbarSeparator(),
			widget.NewToolbarAction(theme.ContentAddIcon(), func() {
				addProfile(state, profileList)
			}),
			widget.NewToolbarAction(theme.DeleteIcon(), func() {
				deleteProfile(state, w, profileList, detailPanel)
			}),
		)

		pathLabel.SetText(state.FilePath)
		header := container.NewBorder(nil, nil, nil, nil, container.NewVBox(toolbar, pathLabel))

		split := container.NewHSplit(profileList, detailPanel.Container)
		split.SetOffset(0.3)

		content := container.NewBorder(header, nil, nil, nil, split)
		w.SetContent(content)
	}

	// Determine which file to open:
	// 1. Last opened file (from preferences)
	// 2. Auto-detected from known locations
	filePath := ""
	lastPath := a.Preferences().String(prefLastFile)
	if lastPath != "" {
		if _, err := LoadProfiles(lastPath); err == nil {
			filePath = lastPath
		}
	}
	if filePath == "" {
		found := FindExistingProfiles()
		if len(found) == 1 {
			filePath = found[0]
		} else if len(found) > 1 {
			// Multiple found — let user pick
			state.Data = &LauncherData{
				Profiles: make(map[string]*Profile),
				Rest:     make(map[string]json.RawMessage),
			}
			buildUI()

			items := make([]string, len(found))
			copy(items, found)
			d := dialog.NewCustomConfirm("Multiple profiles found", "Open", "Browse...",
				widget.NewRadioGroup(items, func(s string) { filePath = s }),
				func(ok bool) {
					if ok && filePath != "" {
						loadFile(state, filePath, w, profileList, detailPanel, pathLabel, a)
					} else {
						// Show file open dialog
						fd := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
							if err != nil || rc == nil {
								return
							}
							rc.Close()
							loadFile(state, rc.URI().Path(), w, profileList, detailPanel, pathLabel, a)
						}, w)
						fd.Show()
					}
				}, w)
			d.Resize(fyne.NewSize(600, 300))
			d.Show()
			w.ShowAndRun()
			return
		}
	}

	if filePath != "" {
		data, err := LoadProfiles(filePath)
		if err == nil {
			state.Data = data
			state.FilePath = filePath
			a.Preferences().SetString(prefLastFile, filePath)
			buildUI()
		} else {
			state.Data = &LauncherData{
				Profiles: make(map[string]*Profile),
				Rest:     make(map[string]json.RawMessage),
			}
			buildUI()
			dialog.ShowError(fmt.Errorf("failed to load %s: %w", filePath, err), w)
		}
	} else {
		// Nothing found anywhere
		state.Data = &LauncherData{
			Profiles: make(map[string]*Profile),
			Rest:     make(map[string]json.RawMessage),
		}
		buildUI()
		dialog.ShowInformation("Welcome",
			"No launcher_profiles.json found.\nUse the Open button to locate it.", w)
	}

	w.ShowAndRun()
}

func loadFile(state *AppState, path string, w fyne.Window, list *widget.List, detail *DetailPanel, pathLabel *widget.Label, a fyne.App) {
	data, err := LoadProfiles(path)
	if err != nil {
		dialog.ShowError(err, w)
		return
	}
	state.Data = data
	state.FilePath = path
	state.SelectedKey = ""
	state.RefreshKeys()
	list.Refresh()
	list.UnselectAll()
	detail.Refresh("")
	pathLabel.SetText(path)
	a.Preferences().SetString(prefLastFile, path)
}

func addProfile(state *AppState, list *widget.List) {
	// Generate random key
	b := make([]byte, 16)
	rand.Read(b)
	key := hex.EncodeToString(b)

	prof := NewProfile()
	state.Data.Profiles[key] = prof
	state.RefreshKeys()
	list.Refresh()

	// Select the new profile
	for i, k := range state.SortedKeys {
		if k == key {
			list.Select(i)
			break
		}
	}
}

func deleteProfile(state *AppState, w fyne.Window, list *widget.List, detail *DetailPanel) {
	if state.SelectedKey == "" {
		return
	}
	prof := state.Data.Profiles[state.SelectedKey]
	name := "this profile"
	if prof != nil {
		name = fmt.Sprintf("'%s'", prof.Name)
	}

	dialog.ShowConfirm("Delete Profile",
		fmt.Sprintf("Are you sure you want to delete %s?", name),
		func(ok bool) {
			if !ok {
				return
			}
			delete(state.Data.Profiles, state.SelectedKey)
			state.SelectedKey = ""
			state.RefreshKeys()
			list.Refresh()
			list.UnselectAll()
			detail.Refresh("")
		}, w)
}
