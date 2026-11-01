# PageTurner TUI review

Open items from the review of the Bubble Tea UI (`ui.go`, `dashboard.go`, `progress.go`, `filemanager.go`).
All reported bugs, library-usage issues and UX suggestions have been addressed; what remains are known limits and one
code suggestion.

## Known limits

- Below roughly 80×24 the dashboard and progress screens overflow by a few lines (their fixed content doesn't fit
  70×20).
- No mouse-wheel scrolling in the full log view, since mouse capture was dropped so text selection works. Keys still
  scroll it, and many terminals turn the wheel into arrow keys on the alt screen.
- The progress bar re-animates from empty when the theme is toggled.

## Code suggestions

- Theme colours are mutable package-level variables. `lipgloss.AdaptiveColor` plus a styles struct passed to the
  screens would be cleaner, but the manual `t` toggle needs to override detection.
