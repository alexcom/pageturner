package main

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Palette holds the UI colours. Each has a light- and a dark-background
// variant that lipgloss picks at render time from its background flag, so the
// theme toggle only flips that flag and no style has to be rebuilt.
type Palette struct {
	Primary    lipgloss.AdaptiveColor
	Secondary  lipgloss.AdaptiveColor
	Text       lipgloss.AdaptiveColor
	Subtext    lipgloss.AdaptiveColor
	Error      lipgloss.AdaptiveColor
	Success    lipgloss.AdaptiveColor
	Warning    lipgloss.AdaptiveColor
	DirBadgeBg lipgloss.AdaptiveColor
	DirBadgeFg lipgloss.AdaptiveColor
	OnPrimary  lipgloss.AdaptiveColor // text on a Primary background (buttons)

	// The progress bar takes fixed colours only, so its gradient is per theme.
	GradientLight [2]string
	GradientDark  [2]string
}

var defaultPalette = Palette{
	Primary:    lipgloss.AdaptiveColor{Light: "#005F73", Dark: "#00ADB5"},
	Secondary:  lipgloss.AdaptiveColor{Light: "#CBD5E1", Dark: "#393E46"},
	Text:       lipgloss.AdaptiveColor{Light: "#0F172A", Dark: "#EEEEEE"},
	Subtext:    lipgloss.AdaptiveColor{Light: "#64748B", Dark: "#AAAAAA"},
	Error:      lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#FF2E63"},
	Success:    lipgloss.AdaptiveColor{Light: "#059669", Dark: "#00D846"},
	Warning:    lipgloss.AdaptiveColor{Light: "#D97706", Dark: "#FFB800"},
	DirBadgeBg: lipgloss.AdaptiveColor{Light: "#005F73", Dark: "#00ADB5"},
	DirBadgeFg: lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1A1A1A"},
	OnPrimary:  lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1A1A1A"},

	GradientLight: [2]string{"#4338CA", "#DB2777"},
	GradientDark:  [2]string{"#5A56E0", "#EE6FF8"}, // bubbles' default gradient
}

// Styles are the shared styles handed to every screen. They never change after
// construction.
//
// lipgloss v0.9 styles share their rules between copies of the value, so a
// setter called on one of these would change it for every screen: call Copy()
// before adjusting one (Width, MaxWidth, Bold...). Render and the Get* methods
// are safe. Bubbles components call setters on their styles while rendering
// (Inline, Reverse), so they always get copies too.
type Styles struct {
	Colors Palette

	Bold    lipgloss.Style
	Accent  lipgloss.Style // focus markers, selected items
	Dim     lipgloss.Style // help text, notes, inactive items
	Warning lipgloss.Style
	Error   lipgloss.Style
	Success lipgloss.Style

	Header   lipgloss.Style
	DirBadge lipgloss.Style

	Dialog      lipgloss.Style // error-framed modal box
	DialogTitle lipgloss.Style
	Panel       lipgloss.Style // progress screen panels
	LogBox      lipgloss.Style // full-log viewport frame
}

func newStyles(p Palette) *Styles {
	fg := func(c lipgloss.TerminalColor) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	return &Styles{
		Colors: p,

		Bold:    lipgloss.NewStyle().Bold(true),
		Accent:  fg(p.Primary),
		Dim:     fg(p.Subtext),
		Warning: fg(p.Warning),
		Error:   fg(p.Error),
		Success: fg(p.Success),

		Header: fg(p.Primary).
			Bold(true).
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Primary),
		DirBadge: lipgloss.NewStyle().
			Background(p.DirBadgeBg).
			Foreground(p.DirBadgeFg).
			Padding(0, 1),

		Dialog: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Error).
			Padding(1, 2),
		DialogTitle: fg(p.Error).Bold(true),
		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Primary).
			Padding(0, 1),
		// Drawn outside the viewport: bubbles' viewport shows Height lines but
		// clips its own frame out of that height, which would hide the newest lines.
		LogBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Secondary).
			PaddingRight(2),
	}
}

// detectTheme makes lipgloss query the terminal background now and cache the
// answer: the query must run before the Bubble Tea program owns the terminal.
func detectTheme() {
	lipgloss.HasDarkBackground()
}

// toggleTheme overrides the detected background, which flips every adaptive colour.
func toggleTheme() {
	lipgloss.SetHasDarkBackground(!lipgloss.HasDarkBackground())
}

func (s *Styles) newHelp() help.Model {
	sep := lipgloss.NewStyle().Foreground(s.Colors.Secondary)
	h := help.New()
	h.Styles = help.Styles{
		ShortKey:       s.Accent.Copy(),
		ShortDesc:      s.Dim.Copy(),
		ShortSeparator: sep,
		FullKey:        s.Accent.Copy(),
		FullDesc:       s.Dim.Copy(),
		FullSeparator:  sep.Copy(),
		Ellipsis:       sep.Copy(),
	}
	return h
}

// newInput returns a themed textinput; its default placeholder colour is a
// fixed grey that ignores the theme.
func (s *Styles) newInput() textinput.Model {
	t := textinput.New()
	t.Cursor.Style = s.Accent.Copy()
	t.PromptStyle = s.Accent.Copy()
	t.PlaceholderStyle = s.Dim.Copy()
	return t
}

func (s *Styles) newSpinner() spinner.Model {
	return spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(s.Accent.Copy()))
}

// themedBar is a progress bar for both themes. bubbles' bar only takes fixed
// gradient colours, so it keeps one bar per theme, animates both and shows the
// one for the current background: toggling the theme doesn't restart the animation.
type themedBar struct {
	light, dark progress.Model
}

func (s *Styles) newBar() themedBar {
	// The percentage is part of the status label, so the bar doesn't print its own.
	bar := func(g [2]string) progress.Model {
		return progress.New(progress.WithGradient(g[0], g[1]), progress.WithoutPercentage())
	}
	return themedBar{light: bar(s.Colors.GradientLight), dark: bar(s.Colors.GradientDark)}
}

func (b *themedBar) SetPercent(p float64) tea.Cmd {
	return tea.Batch(b.light.SetPercent(p), b.dark.SetPercent(p))
}

// Update advances the animation; each bar ignores the other's frames.
func (b *themedBar) Update(msg progress.FrameMsg) tea.Cmd {
	light, lightCmd := b.light.Update(msg)
	dark, darkCmd := b.dark.Update(msg)
	b.light, b.dark = light.(progress.Model), dark.(progress.Model)
	return tea.Batch(lightCmd, darkCmd)
}

// ViewWidth renders the bar for the current theme at the given width.
func (b *themedBar) ViewWidth(w int) string {
	bar := b.light
	if lipgloss.HasDarkBackground() {
		bar = b.dark
	}
	bar.Width = w
	return bar.View()
}
