# PageTurner TUI review

Review of the Bubble Tea UI on `feature/pretty-tui` (`ui.go`, `dashboard.go`, `progress.go`, `filemanager.go`,
plus the pipeline code they drive). Layout claims were checked by rendering the views at 80×24, 100×40 and 120×40.

Status: ✅ fixed · ⏳ open · 💡 suggestion (not scheduled)

## Bugs where the UI doesn't do what it shows

| # | Status | Issue |
|---|--------|-------|
| 1 | ✅ | **Cover selection is ignored.** `resolveCover` never reads `ConversionConfig.CoverPath`/`CoverSource`; it picks its own cover. With "Remove" ticked, `removeSourceFiles` deletes the selected image although it was never embedded (the original CLI never deleted images). |
| 2 | ✅ | **`--remove-source` does nothing.** `collectArguments()` is never called, so `flag.Parse` never runs. The README still documents the flag. |
| 3 | ✅ | **Enter in any dashboard field starts the conversion** (`dashboard.go` Confirm handling). Typing a field and pressing Enter launches a long ffmpeg job. |
| 4 | ✅ | **Full log view stops following new lines.** `AtBottom()` is checked after `SetContent`, so it is always false once logs overflow. Also, the viewport's own border is subtracted from its content height while `visibleLines` still uses the full height, so the 2 newest lines are always clipped. |
| 5 | ✅ | **"Existing .m4b" prompt says `(y/N)` but Enter means yes**, and it fires for *any* `.m4b` in the folder rather than the actual output file. |

## Layout bugs

| # | Status | Issue |
|---|--------|-------|
| 6 | ✅ | **Dashboard doesn't fit 80×24.** It renders 36 lines × 82 columns. `help.Width` ignores the 4-column doc margin (so the whole view is wider than the terminal and wraps), and the left pane's fixed content is taller than the space reserved for it, pushing the header off-screen. |
| 7 | ✅ | **Progress line shows the percentage twice and wraps.** The bubbles progress bar renders its own percentage, and the label (~45 columns) doesn't fit next to a bar sized `panelWidth - 32`. |
| 8 | ✅ | **Errors are appended below the view** (40-row terminal → 44 lines, header scrolls away). The checklist only marks a step failed on cancellation, so after an error it still says "Working...". |
| 9 | ✅ | **Header height hard-coded in three places** (`titleHeight := 6` in file manager and progress, `layoutVerticalNonPaneSpace` in dashboard). A long path or narrow terminal wraps the header/badge and breaks all three. |
| 10 | ✅ | **Truncation ignores real widths.** Worker names cut at a fixed 40 runes, checklist labels wrap at 80 columns, the 5-line log preview isn't truncated (panel height jumps), file names counted in runes instead of display cells. |

## Responsiveness and lifecycle

| # | Status | Issue |
|---|--------|-------|
| 11 | ✅ | **UI freezes when the dashboard opens.** `scanDirectory` runs ffprobe on the first file, on every file (`detectBitrate`) and again for the cover check, synchronously inside `Update`. |
| 12 | ✅ | **File manager is stale after a conversion.** It is reused without reloading (new `.m4b` not highlighted) and the directory badge keeps the old path until the next keypress. |
| 13 | ✅ | **Pipeline goroutine can block forever.** After `msgError` the UI stops reading the updates channel; any later send (e.g. the deferred temp-dir warning) blocks. |

## Fix notes

- **#1** Added an "Auto-detect" cover option (first, default) that keeps the old automatic order: typical cover name → single image → extract → default. Extracted/default covers are now written to the temp dir. Also fixed while there: cleanup paths (`cleanupState`, temp covers) were relative to the process working dir, not the book folder, so files chosen via the file manager were never cleaned up and an unrelated `cover.jpg` in the launch dir could be deleted.
- **#2** Mode may come before or after flags (`pageturner go --remove-source` and `pageturner --remove-source go`); the flag pre-ticks the dashboard toggle.
- **#3** Enter moves to the next field; on the Start button it starts. Shift+Tab from the first field jumps to Start.
- **#6/#9** Dashboard switches to denser layouts (fewer spacer lines, one-line Start button, scrolling cover list) to fit. Header and directory badge are kept to one line (badge truncated from the left). Root cause of the 2-column overflow on every screen: `BorderStyle()` doesn't register border sides, so `GetHorizontalBorderSize()`/`GetFrameSize()` returned 0; switched to `Border()`.
- **#8** A pipeline error no longer exits on the next key: the progress screen shows "Failed", ✗ on the failed step and the error, and `l`/`esc`/`q` keep working. Conversion errors now include the file name. Other fatal errors are a centered dialog.
- **#10** Progress screen also adapts to height (compact layout, sized log preview, "+N more" workers).
- **Known limit:** below roughly 80×24 the dashboard and progress screens still overflow by a few lines (their fixed content doesn't fit 70×20).

## UX suggestions

- 💡 Mouse capture (`tea.WithMouseCellMotion`) blocks text selection; only the lists use the mouse. Drop it or add click-to-focus.
- 💡 Help bar should only show keys that work now (`esc back` during a run, `t theme` while a text field has focus). Use `key.Binding.SetEnabled`. The file manager's `?` full help shows list `h`/`l` paging keys that are overridden, and its extra height isn't accounted for.
- 💡 File manager: Enter on a folder opens the converter instead of entering it, Enter on `..` converts the current folder. Suggest Enter/→ = enter folder, a dedicated key (e.g. `c`) = convert.
- 💡 File manager highlights folders that already have an `.m4b` (no legend). More useful: "N mp3s, ready to convert".
- 💡 "Unknown Artist/Album/Title" are written into the fields (and the tags) instead of being placeholders.
- 💡 Start with no mp3 files silently does nothing; explain why.
- 💡 Detected bitrate is display-only; allow overriding it. If detection fails the pipeline runs with `0k`.
- 💡 Finish screen: show output path, size, duration, and a "convert another" action.
- 💡 Theme: auto-pick initial theme via `lipgloss.HasDarkBackground()` / `AdaptiveColor`; progress gradient doesn't follow the theme; `dirBadgeFg` is reused as button text (add an `OnPrimary` token).

## Code structure suggestions

- ✅ Parent `UI` measures its chrome and sends children a `WindowSizeMsg` for the remaining area (done as part of #6/#9).
- 💡 Theme key defined four times; `uiKeys.Theme` exists but `ui.go` compares raw strings. One shared keymap with contextual `SetEnabled`.
- ✅ Cover choice is a typed `coverItem` (done as part of #1).
- 💡 `FileManagerModel.View` mutates `topOffset`; move to `Update`.
- 💡 `updateViewport()` is called on every focus change/resize but only resets list items.
- 💡 `gofmt -l` lists `meta.go` (all other files were formatted while fixing).
