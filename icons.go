//go:build !headless

package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// Named icon color map for Minecraft launcher built-in icons.
var namedIconColors = map[string]color.NRGBA{
	"Grass":           {R: 89, G: 166, B: 56, A: 255},
	"Dirt":            {R: 134, G: 96, B: 67, A: 255},
	"Cobblestone":     {R: 128, G: 128, B: 128, A: 255},
	"Stone":           {R: 160, G: 160, B: 160, A: 255},
	"Planks":          {R: 188, G: 152, B: 98, A: 255},
	"Iron":            {R: 219, G: 219, B: 219, A: 255},
	"Gold":            {R: 249, G: 236, B: 79, A: 255},
	"Diamond":         {R: 93, G: 236, B: 215, A: 255},
	"Lapis":           {R: 38, G: 67, B: 156, A: 255},
	"Emerald":         {R: 23, G: 196, B: 103, A: 255},
	"Redstone":        {R: 175, G: 30, B: 15, A: 255},
	"TNT":             {R: 219, G: 68, B: 54, A: 255},
	"Bookshelf":       {R: 180, G: 144, B: 90, A: 255},
	"Crafting_Table":  {R: 180, G: 120, B: 60, A: 255},
	"Furnace":         {R: 128, G: 128, B: 128, A: 255},
	"Brick":           {R: 150, G: 97, B: 83, A: 255},
	"Chest":           {R: 172, G: 127, B: 49, A: 255},
	"Pumpkin":         {R: 206, G: 144, B: 32, A: 255},
	"Bedrock":         {R: 85, G: 85, B: 85, A: 255},
	"Glass":           {R: 200, G: 220, B: 230, A: 200},
	"Creeper":         {R: 76, G: 153, B: 76, A: 255},
	"Pig":             {R: 234, G: 176, B: 161, A: 255},
	"Leather":         {R: 140, G: 70, B: 30, A: 255},
	"Log":             {R: 102, G: 78, B: 47, A: 255},
	"Cake":            {R: 230, G: 200, B: 170, A: 255},
}

// decodeBase64Icon decodes a data URI icon into an image.
func decodeBase64Icon(iconStr string) image.Image {
	// Find the base64 payload after the comma
	idx := strings.Index(iconStr, ",")
	if idx < 0 {
		return nil
	}
	b64 := iconStr[idx+1:]
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		// Try generic image decode
		img, _, err = image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil
		}
	}
	return img
}

// generatePlaceholder creates a colored square with the first letter.
func generatePlaceholder(name string, size int) image.Image {
	col, ok := namedIconColors[name]
	if !ok {
		// Hash name to pick a color
		h := 0
		for _, c := range name {
			h = h*31 + int(c)
		}
		if h < 0 {
			h = -h
		}
		col = color.NRGBA{
			R: uint8(60 + h%160),
			G: uint8(60 + (h/256)%160),
			B: uint8(60 + (h/65536)%160),
			A: 255,
		}
	}

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), &image.Uniform{col}, image.Point{}, draw.Src)

	// Draw a simple letter in the center (3x5 pixel font scaled up)
	letter := ' '
	if len(name) > 0 {
		letter = rune(strings.ToUpper(name)[0])
	}
	drawLetter(img, letter, size)

	return img
}

// drawLetter draws a simple pixel letter centered on the image.
func drawLetter(img *image.NRGBA, ch rune, size int) {
	// Simple 3x5 bitmap font for A-Z and 0-9
	glyphs := map[rune][5]uint8{
		'A': {0b111, 0b101, 0b111, 0b101, 0b101},
		'B': {0b110, 0b101, 0b110, 0b101, 0b110},
		'C': {0b111, 0b100, 0b100, 0b100, 0b111},
		'D': {0b110, 0b101, 0b101, 0b101, 0b110},
		'E': {0b111, 0b100, 0b110, 0b100, 0b111},
		'F': {0b111, 0b100, 0b110, 0b100, 0b100},
		'G': {0b111, 0b100, 0b101, 0b101, 0b111},
		'H': {0b101, 0b101, 0b111, 0b101, 0b101},
		'I': {0b111, 0b010, 0b010, 0b010, 0b111},
		'J': {0b001, 0b001, 0b001, 0b101, 0b111},
		'K': {0b101, 0b101, 0b110, 0b101, 0b101},
		'L': {0b100, 0b100, 0b100, 0b100, 0b111},
		'M': {0b101, 0b111, 0b111, 0b101, 0b101},
		'N': {0b101, 0b111, 0b111, 0b111, 0b101},
		'O': {0b111, 0b101, 0b101, 0b101, 0b111},
		'P': {0b111, 0b101, 0b111, 0b100, 0b100},
		'Q': {0b111, 0b101, 0b101, 0b111, 0b001},
		'R': {0b111, 0b101, 0b111, 0b110, 0b101},
		'S': {0b111, 0b100, 0b111, 0b001, 0b111},
		'T': {0b111, 0b010, 0b010, 0b010, 0b010},
		'U': {0b101, 0b101, 0b101, 0b101, 0b111},
		'V': {0b101, 0b101, 0b101, 0b101, 0b010},
		'W': {0b101, 0b101, 0b111, 0b111, 0b101},
		'X': {0b101, 0b101, 0b010, 0b101, 0b101},
		'Y': {0b101, 0b101, 0b010, 0b010, 0b010},
		'Z': {0b111, 0b001, 0b010, 0b100, 0b111},
	}

	glyph, ok := glyphs[ch]
	if !ok {
		return
	}

	// Scale: each pixel of the 3x5 glyph becomes a block
	pixSize := size / 7 // leaves margin
	if pixSize < 1 {
		pixSize = 1
	}
	startX := (size - 3*pixSize) / 2
	startY := (size - 5*pixSize) / 2

	white := color.NRGBA{R: 255, G: 255, B: 255, A: 220}

	for row := 0; row < 5; row++ {
		for col := 0; col < 3; col++ {
			if glyph[row]&(1<<(2-col)) != 0 {
				for dy := 0; dy < pixSize; dy++ {
					for dx := 0; dx < pixSize; dx++ {
						x := startX + col*pixSize + dx
						y := startY + row*pixSize + dy
						if x >= 0 && x < size && y >= 0 && y < size {
							img.SetNRGBA(x, y, white)
						}
					}
				}
			}
		}
	}
}

// ProfileIconImage returns a Fyne canvas image for the given icon value.
func ProfileIconImage(iconValue string, sz float32) *canvas.Image {
	var img image.Image

	if strings.HasPrefix(iconValue, "data:image") {
		img = decodeBase64Icon(iconValue)
	}
	if img == nil {
		img = generatePlaceholder(iconValue, 64)
	}

	ci := canvas.NewImageFromImage(img)
	ci.FillMode = canvas.ImageFillContain
	ci.SetMinSize(fyne.NewSize(sz, sz))
	return ci
}
