package gui

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestTheme(t *testing.T) fyne.Theme {
	t.Helper()
	_ = test.NewApp() // initialise Fyne test environment
	return NewSOTETheme()
}

// hexNRGBA is a helper that parses a hex colour string into color.NRGBA.
// It is used in tests only to keep assertions readable.
func hexNRGBA(s string) color.NRGBA {
	var c color.NRGBA
	c.A = 0xFF
	switch s {
	case "#7C3AED":
		c.R, c.G, c.B = 0x7C, 0x3A, 0xED
	case "#06B6D4":
		c.R, c.G, c.B = 0x06, 0xB6, 0xD4
	case "#10B981":
		c.R, c.G, c.B = 0x10, 0xB9, 0x81
	case "#EF4444":
		c.R, c.G, c.B = 0xEF, 0x44, 0x44
	case "#F59E0B":
		c.R, c.G, c.B = 0xF5, 0x9E, 0x0B
	case "#1A1B26":
		c.R, c.G, c.B = 0x1A, 0x1B, 0x26
	case "#A9B1D6":
		c.R, c.G, c.B = 0xA9, 0xB1, 0xD6
	case "#565F89":
		c.R, c.G, c.B = 0x56, 0x5F, 0x89
	case "#FFFFFF":
		c.R, c.G, c.B = 0xFF, 0xFF, 0xFF
	case "#000000":
		c.R, c.G, c.B = 0x00, 0x00, 0x00
	}
	return c
}

func TestThemeColors(t *testing.T) {
	th := setupTestTheme(t)

	tests := []struct {
		name      string
		colorName fyne.ThemeColorName
		want      color.NRGBA
	}{
		{"Primary = #7C3AED", theme.ColorNamePrimary, hexNRGBA("#7C3AED")},
		{"Hyperlink = #7C3AED", theme.ColorNameHyperlink, hexNRGBA("#7C3AED")},
		{"Background = #1A1B26", theme.ColorNameBackground, hexNRGBA("#1A1B26")},
		{"Foreground = #A9B1D6", theme.ColorNameForeground, hexNRGBA("#A9B1D6")},
		{"Error = #EF4444", theme.ColorNameError, hexNRGBA("#EF4444")},
		{"Success = #10B981", theme.ColorNameSuccess, hexNRGBA("#10B981")},
		{"Warning = #F59E0B", theme.ColorNameWarning, hexNRGBA("#F59E0B")},
		{"PlaceHolder = #565F89", theme.ColorNamePlaceHolder, hexNRGBA("#565F89")},
		{"Button = #7C3AED", theme.ColorNameButton, hexNRGBA("#7C3AED")},
		{"ForegroundOnPrimary = white", theme.ColorNameForegroundOnPrimary, hexNRGBA("#FFFFFF")},
		{"InputBorder = #06B6D4", theme.ColorNameInputBorder, hexNRGBA("#06B6D4")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := th.Color(tt.colorName, theme.VariantDark)
			assert.Equal(t, tt.want, got, tt.name)
		})
	}
}

func TestThemeFonts(t *testing.T) {
	th := setupTestTheme(t)

	styles := []fyne.TextStyle{
		{},
		{Bold: true},
		{Italic: true},
		{Bold: true, Italic: true},
		{Monospace: true},
	}

	for _, s := range styles {
		r := th.Font(s)
		require.NotNil(t, r, "Font(%+v) should not return nil", s)
		assert.NotEmpty(t, r.Name(), "Font(%+v) name should not be empty", s)
	}
}

func TestThemeSizes(t *testing.T) {
	th := setupTestTheme(t)

	sizes := []fyne.ThemeSizeName{
		theme.SizeNameText,
		theme.SizeNameHeadingText,
		theme.SizeNameSubHeadingText,
		theme.SizeNameCaptionText,
		theme.SizeNamePadding,
		theme.SizeNameInlineIcon,
		theme.SizeNameScrollBar,
		theme.SizeNameInputBorder,
	}

	for _, name := range sizes {
		s := th.Size(name)
		assert.Greater(t, s, float32(0), "Size(%s) should be > 0", name)
	}
}

func TestThemeMatchesTUI(t *testing.T) {
	th := setupTestTheme(t)

	assert.Equal(t, hexNRGBA("#7C3AED"), th.Color(theme.ColorNamePrimary, theme.VariantDark),
		"primary must match TUI ColorPrimary #7C3AED")
	assert.Equal(t, hexNRGBA("#06B6D4"), th.Color(theme.ColorNameInputBorder, theme.VariantDark),
		"secondary must match TUI ColorSecondary #06B6D4")
	assert.Equal(t, hexNRGBA("#10B981"), th.Color(theme.ColorNameSuccess, theme.VariantDark),
		"success must match TUI ColorSuccess #10B981")
	assert.Equal(t, hexNRGBA("#EF4444"), th.Color(theme.ColorNameError, theme.VariantDark),
		"error must match TUI ColorError #EF4444")
	assert.Equal(t, hexNRGBA("#F59E0B"), th.Color(theme.ColorNameWarning, theme.VariantDark),
		"warning must match TUI ColorWarning #F59E0B")
	assert.Equal(t, hexNRGBA("#1A1B26"), th.Color(theme.ColorNameBackground, theme.VariantDark),
		"background must match TUI ColorBackground #1A1B26")
	assert.Equal(t, hexNRGBA("#A9B1D6"), th.Color(theme.ColorNameForeground, theme.VariantDark),
		"text must match TUI ColorText #A9B1D6")
	assert.Equal(t, hexNRGBA("#565F89"), th.Color(theme.ColorNamePlaceHolder, theme.VariantDark),
		"muted must match TUI ColorMuted #565F89")
}

func TestThemeDarkVariantOnly(t *testing.T) {
	th := setupTestTheme(t)

	assert.Equal(t, th.Color(theme.ColorNameBackground, theme.VariantDark),
		th.Color(theme.ColorNameBackground, theme.VariantLight),
		"dark variant should return same colors regardless of variant parameter")

	assert.Equal(t, th.Color(theme.ColorNamePrimary, theme.VariantDark),
		th.Color(theme.ColorNamePrimary, theme.VariantLight),
		"primary color should be dark-only regardless of variant")

	assert.Equal(t, th.Color(theme.ColorNameForeground, theme.VariantDark),
		th.Color(theme.ColorNameForeground, theme.VariantLight),
		"foreground color should be dark-only regardless of variant")
}

func TestThemeImplementsInterface(t *testing.T) {
	th := NewSOTETheme()
	require.Implements(t, (*fyne.Theme)(nil), th, "SOTETheme must implement fyne.Theme")
}

func TestThemeIcon(t *testing.T) {
	th := setupTestTheme(t)

	icons := []fyne.ThemeIconName{
		theme.IconNameCancel,
		theme.IconNameConfirm,
		theme.IconNameSearch,
		theme.IconNameMenu,
	}

	for _, name := range icons {
		r := th.Icon(name)
		require.NotNil(t, r, "Icon(%s) should not return nil", name)
	}
}

func TestThemeColorFallback(t *testing.T) {
	th := setupTestTheme(t)

	c := th.Color(fyne.ThemeColorName("nonexistent"), theme.VariantDark)
	require.NotNil(t, c, "unknown color name should fall back to default theme")
}

func TestNewSOTETheme_IsNotNil(t *testing.T) {
	th := NewSOTETheme()
	require.NotNil(t, th, "NewSOTETheme should return a non-nil theme")
}
