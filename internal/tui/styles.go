package tui

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
)

// ─── Color Palette ───────────────────────────────────────────────────────────

// Color definitions for the dark-first color scheme.
// All colors are defined once here and referenced by name throughout the TUI.
var (
	ColorPrimary    color.Color = lipgloss.Color("#7C3AED") // Purple
	ColorSecondary  color.Color = lipgloss.Color("#06B6D4") // Cyan
	ColorSuccess    color.Color = lipgloss.Color("#10B981") // Green
	ColorError      color.Color = lipgloss.Color("#EF4444") // Red
	ColorWarning    color.Color = lipgloss.Color("#F59E0B") // Amber
	ColorBackground color.Color = lipgloss.Color("#1A1B26") // Dark navy
	ColorText       color.Color = lipgloss.Color("#A9B1D6") // Light grayish blue
	ColorMuted      color.Color = lipgloss.Color("#565F89") // Dim blue-gray
	ColorWhite      color.Color = lipgloss.Color("#FFFFFF")
	ColorBlack      color.Color = lipgloss.Color("#000000")

	// lightDark is the adaptive color function, initialized in init().
	lightDark lipgloss.LightDarkFunc
)

// init detects the terminal background and prepares adaptive color support.
func init() {
	hasDark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	lightDark = lipgloss.LightDark(hasDark)
}

// AdaptiveColor returns the appropriate color for the current terminal
// background. On dark backgrounds, returns `dark`. On light, returns `light`.
func AdaptiveColor(light, dark color.Color) color.Color {
	return lightDark(light, dark)
}

// ─── Layout Constants ────────────────────────────────────────────────────────

const (
	// MinTerminalWidth is the minimum supported terminal width (80 columns).
	MinTerminalWidth = 80
	// MinTerminalHeight is the minimum supported terminal height (24 rows).
	MinTerminalHeight = 24

	// DefaultPadding is the standard inner padding for containers.
	DefaultPadding = 1
	// DefaultMargin is the standard outer margin for containers.
	DefaultMargin = 1
)

// ─── Responsive Layout Helpers ───────────────────────────────────────────────

// ResponsiveWidth calculates a width based on a fraction of the total available
// width, clamped to the terminal minimum. Fraction should be between 0.0 and 1.0.
// All width calculations use lipgloss math, not magic numbers.
func ResponsiveWidth(totalWidth int, fraction float64) int {
	if fraction <= 0 || fraction > 1.0 {
		fraction = 1.0
	}
	calculated := int(float64(totalWidth) * fraction)
	if calculated < MinTerminalWidth {
		return MinTerminalWidth
	}
	return calculated
}

// ContainerWidth returns a width suitable for a content container, accounting
// for border and padding overhead.
func ContainerWidth(contentWidth, borderWidth, padding int) int {
	return contentWidth + (borderWidth * 2) + (padding * 2)
}

// ─── Base Style ──────────────────────────────────────────────────────────────

// BaseStyle is the foundational style applied to all views. Provides the dark
// theme's background and text color.
var BaseStyle = lipgloss.NewStyle().
	Background(ColorBackground).
	Foreground(ColorText)

// ─── Component Styles ────────────────────────────────────────────────────────

// HeaderStyle is for view titles and headers. Bold, centered, purple background
// with white text. Designed to fill the full terminal width.
var HeaderStyle = lipgloss.NewStyle().
	Bold(true).
	Background(ColorPrimary).
	Foreground(ColorWhite).
	Padding(0, 1).
	AlignHorizontal(lipgloss.Center)

// StatusBarStyle is for the bottom status bar. Dim text on a muted background,
// full terminal width.
var StatusBarStyle = lipgloss.NewStyle().
	Background(ColorMuted).
	Foreground(ColorText).
	Padding(0, 1).
	Faint(true)

// ─── Text Styles ─────────────────────────────────────────────────────────────

// TextStyle is the default text style with the light foreground color.
var TextStyle = lipgloss.NewStyle().Foreground(ColorText)

// MutedStyle is for dim, secondary text.
var MutedStyle = lipgloss.NewStyle().Foreground(ColorMuted)

// PrimaryStyle is for text in the primary color.
var PrimaryStyle = lipgloss.NewStyle().Foreground(ColorPrimary)

// SecondaryStyle is for text in the secondary color.
var SecondaryStyle = lipgloss.NewStyle().Foreground(ColorSecondary)

// ErrorStyle is for error messages: bold red text.
var ErrorStyle = lipgloss.NewStyle().
	Foreground(ColorError).
	Bold(true)

// SuccessStyle is for success messages: bold green text.
var SuccessStyle = lipgloss.NewStyle().
	Foreground(ColorSuccess).
	Bold(true)

// WarningStyle is for warning messages: amber text.
var WarningStyle = lipgloss.NewStyle().
	Foreground(ColorWarning)

// ─── Input Styles ────────────────────────────────────────────────────────────

// InputStyleFocused is for the currently focused text input. Uses a cyan border
// to indicate focus.
var InputStyleFocused = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorSecondary).
	Padding(0, 1).
	Foreground(ColorText).
	Background(ColorBackground)

// InputStyleBlurred is for unfocused text inputs. Uses a dim border.
var InputStyleBlurred = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorMuted).
	Padding(0, 1).
	Foreground(ColorMuted).
	Background(ColorBackground)

// ─── List Styles ─────────────────────────────────────────────────────────────

// ListStyleSelected is for the currently selected/highlighted list item.
var ListStyleSelected = lipgloss.NewStyle().
	Background(ColorPrimary).
	Foreground(ColorWhite).
	Padding(0, 1).
	Bold(true)

// ListStyleUnselected is for non-selected list items.
var ListStyleUnselected = lipgloss.NewStyle().
	Foreground(ColorText).
	Padding(0, 1)

// ─── Button Styles ───────────────────────────────────────────────────────────

// ButtonStylePrimary is for primary action buttons (purple background).
var ButtonStylePrimary = lipgloss.NewStyle().
	Background(ColorPrimary).
	Foreground(ColorWhite).
	Padding(0, 2).
	Bold(true).
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorPrimary)

// ButtonStyleDanger is for destructive action buttons (red background).
var ButtonStyleDanger = lipgloss.NewStyle().
	Background(ColorError).
	Foreground(ColorWhite).
	Padding(0, 2).
	Bold(true).
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorError)

// ButtonStyleDefault is for neutral/default buttons.
var ButtonStyleDefault = lipgloss.NewStyle().
	Background(ColorMuted).
	Foreground(ColorWhite).
	Padding(0, 2).
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorMuted)

// ─── Card Style ──────────────────────────────────────────────────────────────

// CardStyle is a bordered, padded container for grouping related content.
var CardStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorMuted).
	Padding(1, 2).
	Background(ColorBackground).
	Foreground(ColorText)

// ─── Help Style ──────────────────────────────────────────────────────────────

// HelpStyle is for help text and keyboard shortcuts: dim, italic.
var HelpStyle = lipgloss.NewStyle().
	Foreground(ColorMuted).
	Italic(true)

// ─── Title/Heading Styles ────────────────────────────────────────────────────

// TitleStyle is a large, bold, centered title with primary color.
var TitleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(ColorPrimary).
	AlignHorizontal(lipgloss.Center).
	Padding(0, 1)

// SubtitleStyle is a smaller, dimmer heading.
var SubtitleStyle = lipgloss.NewStyle().
	Foreground(ColorMuted).
	AlignHorizontal(lipgloss.Center).
	Padding(0, 1)

// ─── Menu Styles ─────────────────────────────────────────────────────────────

// MenuTitleStyle is the title shown at the top of the main menu.
var MenuTitleStyle = lipgloss.NewStyle().
	Foreground(ColorSecondary).
	Bold(true).
	Padding(0, 1).
	AlignHorizontal(lipgloss.Center)

// MenuItemStyle is an unselected menu entry.
var MenuItemStyle = lipgloss.NewStyle().
	Foreground(ColorText).
	Padding(0, 1)

// MenuItemSelectedStyle is the currently highlighted menu entry.
var MenuItemSelectedStyle = lipgloss.NewStyle().
	Foreground(ColorPrimary).
	Background(lipgloss.Color("#2D2E3E")).
	Bold(true).
	Padding(0, 1)

// MenuHelpStyle is the keyboard shortcut hint line below the menu.
var MenuHelpStyle = lipgloss.NewStyle().
	Foreground(ColorMuted).
	PaddingTop(1).
	AlignHorizontal(lipgloss.Center)

// ─── Settings Styles ─────────────────────────────────────────────────────────

// SectionTitleStyle labels a settings section.
var SectionTitleStyle = lipgloss.NewStyle().
	Foreground(ColorSecondary).
	Bold(true).
	Padding(0, 0, 1, 0)

// LabelStyle renders setting label text.
var LabelStyle = lipgloss.NewStyle().
	Foreground(ColorMuted).
	Width(20)

// ValueStyle renders setting value text.
var ValueStyle = lipgloss.NewStyle().
	Foreground(ColorText)

// StatusOnlineStyle renders an "online" status indicator.
var StatusOnlineStyle = lipgloss.NewStyle().
	Foreground(ColorSuccess).
	Bold(true)

// StatusOfflineStyle renders an "offline" status indicator.
var StatusOfflineStyle = lipgloss.NewStyle().
	Foreground(ColorError).
	Bold(true)

// ─── Chat Styles ─────────────────────────────────────────────────────────────

// ContactHeaderStyle is for the contact selector bar at the top of the chat view.
var ContactHeaderStyle = lipgloss.NewStyle().
	Foreground(ColorText).
	Background(ColorPrimary).
	Bold(true).
	Padding(0, 1)

// SentMsgStyle is for own messages: right-aligned, primary background.
var SentMsgStyle = lipgloss.NewStyle().
	Foreground(ColorWhite).
	Background(ColorPrimary).
	Padding(0, 1)

// ReceivedMsgStyle is for messages from contacts: left-aligned, secondary background.
var ReceivedMsgStyle = lipgloss.NewStyle().
	Foreground(ColorWhite).
	Background(ColorSecondary).
	Padding(0, 1)

// TimestampStyle is a dimmed timestamp prefix for chat messages.
var TimestampStyle = lipgloss.NewStyle().
	Foreground(ColorMuted)

// ChatInputAreaStyle is for the chat textarea input area.
var ChatInputAreaStyle = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder(), false, false, true, false).
	BorderForeground(ColorPrimary).
	Padding(0, 1)

// ─── Login Styles ────────────────────────────────────────────────────────────

// CenterBlock wraps lipgloss.Place to center content within terminal dimensions.
func CenterBlock(width, height int, content string) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

// PromptStyle is used for the login/register prompt text.
var PromptStyle = lipgloss.NewStyle().
	Foreground(ColorMuted).
	Bold(true)

// PromptFocusedStyle is used for the currently focused prompt.
var PromptFocusedStyle = lipgloss.NewStyle().
	Foreground(ColorPrimary).
	Bold(true)

// ButtonGhostStyle is a border-only (outline) button style.
var ButtonGhostStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorPrimary).
	Foreground(ColorPrimary).
	Padding(0, 2).
	Bold(true)

// ButtonStyle is a primary button alias — same as ButtonStylePrimary.
var ButtonStyle = ButtonStylePrimary

// ─── Helper Functions ────────────────────────────────────────────────────────

// StatusDot returns a styled status indicator for the given state.
func StatusDot(online, pending bool) string {
	switch {
	case pending:
		return WarningStyle.Render("\u25CF")
	case online:
		return SuccessStyle.Render("\u25CF")
	default:
		return MutedStyle.Render("\u25CF")
	}
}

// RenderEmptyState returns a centered, muted message for empty views.
// Uses lipgloss.Place for responsive positioning based on the provided width.
func RenderEmptyState(width int, message string) string {
	return lipgloss.Place(
		width, 3,
		lipgloss.Center, lipgloss.Center,
		MutedStyle.Italic(true).Render(message),
	)
}
