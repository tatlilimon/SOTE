//go:build !nogui

package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

var soteColorPalette = map[fyne.ThemeColorName]color.Color{
	theme.ColorNamePrimary:            color.NRGBA{R: 0x7C, G: 0x3A, B: 0xED, A: 0xFF}, // #7C3AED
	theme.ColorNameHyperlink:          color.NRGBA{R: 0x7C, G: 0x3A, B: 0xED, A: 0xFF}, // #7C3AED
	theme.ColorNameBackground:         color.NRGBA{R: 0x1A, G: 0x1B, B: 0x26, A: 0xFF}, // #1A1B26
	theme.ColorNameForeground:         color.NRGBA{R: 0xA9, G: 0xB1, B: 0xD6, A: 0xFF}, // #A9B1D6
	theme.ColorNameError:              color.NRGBA{R: 0xEF, G: 0x44, B: 0x44, A: 0xFF}, // #EF4444
	theme.ColorNameSuccess:            color.NRGBA{R: 0x10, G: 0xB9, B: 0x81, A: 0xFF}, // #10B981
	theme.ColorNameWarning:            color.NRGBA{R: 0xF5, G: 0x9E, B: 0x0B, A: 0xFF}, // #F59E0B
	theme.ColorNamePlaceHolder:        color.NRGBA{R: 0x56, G: 0x5F, B: 0x89, A: 0xFF}, // #565F89
	theme.ColorNameDisabled:           color.NRGBA{R: 0x56, G: 0x5F, B: 0x89, A: 0x80},
	theme.ColorNameDisabledButton:     color.NRGBA{R: 0x56, G: 0x5F, B: 0x89, A: 0x80},
	theme.ColorNameForegroundOnPrimary: color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF},
	theme.ColorNameForegroundOnError:   color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF},
	theme.ColorNameForegroundOnSuccess: color.NRGBA{R: 0x1A, G: 0x1B, B: 0x26, A: 0xFF},
	theme.ColorNameForegroundOnWarning: color.NRGBA{R: 0x1A, G: 0x1B, B: 0x26, A: 0xFF},
	theme.ColorNameButton:              color.NRGBA{R: 0x7C, G: 0x3A, B: 0xED, A: 0xFF}, // #7C3AED
	theme.ColorNameHover:               color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x0F},
	theme.ColorNameFocus:               color.NRGBA{R: 0x7C, G: 0x3A, B: 0xED, A: 0x40},
	theme.ColorNamePressed:             color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x66},
	theme.ColorNameSelection:           color.NRGBA{R: 0x7C, G: 0x3A, B: 0xED, A: 0x40},
	theme.ColorNameInputBackground:     color.NRGBA{R: 0x20, G: 0x21, B: 0x2B, A: 0xFF},
	theme.ColorNameInputBorder:         color.NRGBA{R: 0x06, G: 0xB6, B: 0xD4, A: 0xFF}, // #06B6D4
	theme.ColorNameHeaderBackground:    color.NRGBA{R: 0x1A, G: 0x1B, B: 0x26, A: 0xFF},
	theme.ColorNameMenuBackground:      color.NRGBA{R: 0x23, G: 0x24, B: 0x33, A: 0xFF},
	theme.ColorNameOverlayBackground:   color.NRGBA{R: 0x1A, G: 0x1B, B: 0x26, A: 0xFF},
	theme.ColorNameScrollBar:           color.NRGBA{R: 0x56, G: 0x5F, B: 0x89, A: 0x99},
	theme.ColorNameScrollBarBackground: color.NRGBA{R: 0x1A, G: 0x1B, B: 0x26, A: 0xFF},
	theme.ColorNameSeparator:           color.NRGBA{R: 0x06, G: 0xB6, B: 0xD4, A: 0x40},
	theme.ColorNameShadow:              color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x66},
}

// SOTETheme is a dark Fyne theme matching the TUI Lipgloss palette.
type SOTETheme struct {
	base fyne.Theme
}

// NewSOTETheme returns a SOTETheme that matches the TUI colour scheme.
func NewSOTETheme() fyne.Theme {
	return &SOTETheme{base: theme.DefaultTheme()}
}

// Color returns the colour for the given name. Variant is ignored — dark palette always.
func (t *SOTETheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	if c, ok := soteColorPalette[name]; ok {
		return c
	}
	return t.base.Color(name, theme.VariantDark)
}

func (t *SOTETheme) Font(style fyne.TextStyle) fyne.Resource  { return t.base.Font(style) }
func (t *SOTETheme) Icon(name fyne.ThemeIconName) fyne.Resource { return t.base.Icon(name) }
func (t *SOTETheme) Size(name fyne.ThemeSizeName) float32       { return t.base.Size(name) }
