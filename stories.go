package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	tea "github.com/charmbracelet/bubbletea"
)

// ConversionMode says how the MP3 files of a run become .m4b files.
type ConversionMode int

const (
	ModeBook    ConversionMode = iota // all MP3s of a folder, one chapter each, merged into one .m4b
	ModeStories                       // every MP3 becomes its own .m4b next to it
)

func (m ConversionMode) String() string {
	if m == ModeStories {
		return "stories"
	}
	return "book"
}

// storyOutputPath is where the .m4b for one story is written: next to its MP3.
func storyOutputPath(src string) string {
	return strings.TrimSuffix(src, filepath.Ext(src)) + ".m4b"
}

// story holds the files of one story while it moves through the pipeline.
type story struct {
	src   string // source mp3 (absolute)
	m4a   string // converted audio in the temp dir
	cover string // image to embed
	meta  string // FFMETA file in the temp dir
	out   string // resulting .m4b
}

// runStoriesPipeline converts every file in config.Files into its own .m4b.
// It has the same steps as the book pipeline, each one run in parallel.
func runStoriesPipeline(ctx context.Context, config ConversionConfig, updates chan tea.Msg) {
	defer close(updates)

	if len(config.Files) == 0 {
		updates <- msgLog{text: "No MP3 files to convert."}
		updates <- msgCancelled{}
		return
	}

	updates <- msgLog{text: "Checking prerequisites..."}
	updates <- msgStepAdvance{step: 0}
	if err := checkPrerequisites(); err != nil {
		updates <- msgError{err: err}
		return
	}
	if config.BitRate <= 0 {
		updates <- msgError{err: fmt.Errorf("no bitrate set (it could not be detected from the MP3 files)")}
		return
	}

	convertDir, err := os.MkdirTemp(os.TempDir(), "pageturner")
	if err != nil {
		updates <- msgError{err: err}
		return
	}
	cleanupState.setConvertDir(convertDir)
	defer func() {
		if err := os.RemoveAll(convertDir); err != nil {
			updates <- msgLog{text: "WARN " + err.Error()}
		}
	}()

	stories := make([]story, len(config.Files))
	for i, src := range config.Files {
		stories[i] = story{
			src:  src,
			m4a:  filepath.Join(convertDir, fmt.Sprintf("%04d.m4a", i)),
			meta: filepath.Join(convertDir, fmt.Sprintf("%04d.ffmeta", i)),
			out:  storyOutputPath(src),
		}
	}
	updates <- msgLog{text: fmt.Sprintf("Converting %d stories at %d kbps", len(stories), config.BitRate)}

	// forEach builds one pool job per story.
	forEach := func(step int, verb string, work func(st *story, i int) error) error {
		jobs := make([]poolJob, len(stories))
		for i := range stories {
			i, st := i, &stories[i]
			name := filepath.Base(st.src)
			jobs[i] = poolJob{label: name, done: verb + " " + name, run: func() error { return work(st, i) }}
		}
		return runPool(ctx, step, jobs, updates)
	}

	steps := []struct {
		log, verb string
		work      func(st *story, i int) error
	}{
		{"Converting files...", "converted", func(st *story, _ int) error {
			return runScriptArgs(ctx, filepath.Dir(st.src), ffmpeg, convertArgs(st.src, st.m4a, config.BitRate), nil)
		}},
		{"Resolving cover art...", "", newStoryCovers(ctx, config, convertDir, updates).resolve},
		{"Generating metadata files...", "", func(st *story, _ int) error {
			return writeStoryMeta(ctx, config, st)
		}},
		{"Merging stories...", "wrote", func(st *story, _ int) error {
			cleanupState.addOut(st.out)
			if err := mergeStory(ctx, st); err != nil {
				return err
			}
			cleanupState.removeOut(st.out)
			return nil
		}},
	}
	for i, s := range steps {
		step := i + 1
		updates <- msgStepAdvance{step: step}
		updates <- msgLog{text: s.log}
		if err := forEach(step, s.verb, s.work); err != nil {
			updates <- msgError{err: err}
			return
		}
	}

	updates <- msgStepAdvance{step: 5}
	updates <- msgLog{text: "Cleaning up..."}
	if config.RemoveSource {
		updates <- msgLog{text: "Source removal requested"}
		for _, st := range stories {
			if err := os.Remove(st.src); err != nil {
				updates <- msgLog{text: fmt.Sprintf("error when deleting the source file %q: %v", st.src, err)}
			}
		}
	}

	outputs := make([]outputInfo, len(stories))
	for i, st := range stories {
		outputs[i] = describeOutput(ctx, st.out)
	}
	updates <- msgLog{text: "Conversion successful!"}
	updates <- msgStepAdvance{step: 6}
	updates <- msgConversionDone{outputs: outputs}
}

// storyCovers picks a cover per story. Folder images and the default cover are
// looked up once and shared; embedded pictures are extracted per story.
type storyCovers struct {
	ctx        context.Context
	config     ConversionConfig
	convertDir string
	updates    chan<- tea.Msg

	mu        sync.Mutex
	folder    map[string]string // dir -> cover image found there ("" = none)
	defaultFn string
}

func newStoryCovers(ctx context.Context, config ConversionConfig, convertDir string, updates chan<- tea.Msg) *storyCovers {
	return &storyCovers{ctx: ctx, config: config, convertDir: convertDir, updates: updates, folder: map[string]string{}}
}

func (c *storyCovers) resolve(st *story, i int) error {
	kind := c.config.CoverKind
	if kind == CoverFile {
		if _, err := os.Stat(c.config.CoverPath); err == nil {
			st.cover = c.config.CoverPath
			return nil
		}
		sendLog(c.updates, "WARN selected cover "+c.config.CoverPath+" not found, detecting automatically")
		kind = CoverAuto
	}
	if kind == CoverExtract || kind == CoverAuto {
		if path := c.extract(st.src, i); path != "" {
			st.cover = path
			return nil
		}
	}
	if kind == CoverAuto {
		if path := c.folderCover(filepath.Dir(st.src)); path != "" {
			st.cover = path
			return nil
		}
	}
	path, err := c.defaultCover()
	st.cover = path
	return err
}

// extract writes the picture embedded in src, if it has one.
func (c *storyCovers) extract(src string, i int) string {
	dir, name := filepath.Dir(src), filepath.Base(src)
	if !hasCoverImage(c.ctx, dir, name) { // checked first: a failed ffmpeg run leaves a fail log
		return ""
	}
	out := filepath.Join(c.convertDir, fmt.Sprintf("%04d.jpg", i))
	if err := runScriptArgs(c.ctx, dir, ffmpeg, []string{confirm, input, src, noAudio, out}, nil); err != nil {
		sendLog(c.updates, "cover extraction failed for "+name+": "+err.Error())
		return ""
	}
	return out
}

func (c *storyCovers) folderCover(dir string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if path, ok := c.folder[dir]; ok {
		return path
	}
	path := ""
	if name, err := findCover(dir, nil); err == nil && name != "" {
		path = filepath.Join(dir, name)
		sendLog(c.updates, "Using folder cover "+path)
	}
	c.folder[dir] = path
	return path
}

func (c *storyCovers) defaultCover() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.defaultFn == "" {
		path, err := writeDefaultCover(c.convertDir)
		if err != nil {
			return "", err
		}
		c.defaultFn = path
	}
	return c.defaultFn, nil
}

// writeStoryMeta writes the FFMETA file of one story: its own tags (with the
// dashboard's non-empty fields overriding them) and a single chapter.
func writeStoryMeta(ctx context.Context, config ConversionConfig, st *story) error {
	bb, err := getMetaJsonBytes(ctx, "", st.m4a)
	if err != nil {
		return fmt.Errorf("reading converted audio: %w", err)
	}
	var probe container
	var tags tagsContainer
	if err := json.Unmarshal(bb.Bytes(), &probe); err != nil {
		return err
	}
	if err := json.Unmarshal(bb.Bytes(), &tags); err != nil {
		return err
	}
	if tags.Format.Tags == nil {
		tags.Format.Tags = map[string]string{}
	}
	removeNonWhitelistedTags(&tags)
	delete(tags.Format.Tags, "track") // a story is a book of its own

	// The m4a is named 0001.m4a, so the title falls back to the mp3's name.
	probe.Format.Filename = st.src
	title := selectTitle(probe.Format, 0)
	t := tags.Format.Tags
	t["title"] = title
	t["genre"] = "Audiobook"
	for key, override := range map[string]string{"artist": config.Artist, "album": config.Album, "performer": config.Performer} {
		if override != "" {
			t[key] = override
		}
	}

	_, end, err := parseAppendDuration(0, probe.Format.Duration)
	if err != nil {
		return fmt.Errorf("reading duration: %w", err)
	}
	data := struct {
		Chapters   []track
		CommonMeta map[string]string
	}{
		Chapters:   []track{{Title: title, Start: 0, End: end}},
		CommonMeta: t,
	}
	file, err := os.OpenFile(st.meta, newFileMode, 0644)
	if err != nil {
		return err
	}
	defer closeDeferred(file)
	return template.Must(template.New("ffmetadata").Parse(ffmetadataTemplate)).Execute(file, data)
}

// mergeStory muxes the story's audio, cover and metadata into its .m4b.
func mergeStory(ctx context.Context, st *story) error {
	args := []string{
		confirm,
		input, st.m4a,
		input, st.cover,
		input, st.meta,
		mapping, "0:a",
		mapping, "1",
		mapMeta, "2",
		codec, codecCopy,
		dispositionV0, attachedPic,
		st.out,
	}
	return runScriptArgs(ctx, filepath.Dir(st.out), ffmpeg, args, nil)
}
