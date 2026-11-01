# PageTurner TUI review

Open items from the review of the Bubble Tea UI (`ui.go`, `dashboard.go`, `progress.go`, `filemanager.go`).
All reported bugs and library-usage issues have been fixed; what remains are known limits and suggestions.

## Known limits

- Below roughly 80×24 the dashboard and progress screens overflow by a few lines (their fixed content doesn't fit
  70×20).
- No mouse-wheel scrolling in the full log view, since mouse capture was dropped so text selection works. Keys still
  scroll it, and many terminals turn the wheel into arrow keys on the alt screen.
- The progress bar re-animates from empty when the theme is toggled.

## UX suggestions

- File manager: Enter on a folder opens the converter instead of entering it, and Enter on `..` converts the current
  folder. Suggest Enter/→ = enter folder, a dedicated key (e.g. `c`) = convert.
- File manager highlights folders that already have an `.m4b`, with no legend. More useful: "N mp3s, ready to
  convert".
- "Unknown Artist/Album/Title" are written into the fields (and the tags) instead of being placeholders.
- Start with no mp3 files silently does nothing; explain why.
- Detected bitrate is display-only; allow overriding it. If detection fails the pipeline runs with `0k`.
- Finish screen: show output path, size, duration, and a "convert another" action.

## Code suggestions

- Theme: `dirBadgeFg` is reused as button text; add an `OnPrimary` token.
- Theme colours are mutable package-level variables. `lipgloss.AdaptiveColor` plus a styles struct passed to the
  screens would be cleaner, but the manual `t` toggle needs to override detection.
- `gofmt -l` lists `meta.go`.
