package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

const maxLogLines = 500

var (
	handBrakeTask   = regexp.MustCompile(`(?i)Encoding:\s*task\s+(\d+)\s+of\s+(\d+).*?(\d+(?:\.\d+)?)\s*%`)
	handBrakeStatus = regexp.MustCompile(`(?i)^(?:Encoding|Scanning|Muxing):.*\d+(?:\.\d+)?\s*%`)
	plainPercent    = regexp.MustCompile(`(?i)\b(\d+(?:\.\d+)?)\s*%`)
	ffmpegDuration  = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)
	ffmpegTime      = regexp.MustCompile(`(?:time|out_time)=\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)
	ffmpegOutTime   = regexp.MustCompile(`out_time_(?:us|ms)=(\d+)`)
	ffmpegProgress  = regexp.MustCompile(`^(?:frame|fps|stream_\d+_\d+_q|bitrate|total_size|out_time_(?:us|ms)|out_time|dup_frames|drop_frames|speed|progress)=`)
)

// Dashboard owns all terminal output while an interactive conversion is running.
// It falls back to ordinary line output when stdout is redirected or not a terminal.
type Dashboard struct {
	mu          sync.Mutex
	interactive bool
	out         io.Writer
	in          *os.File
	oldState    *term.State
	stop        chan struct{}
	quit        chan struct{}
	stopOnce    sync.Once
	quitOnce    sync.Once
	renderWG    sync.WaitGroup

	total, done, failed int
	events              []string
	workers             []*workerState
	workerByID          map[string]*workerState
	selected            int
	lastFrame           []string
	lastWidth           int
	lastHeight          int
	outputRows          int
}

type workerState struct {
	id, node, job, status string
	progress              float64
	progressKnown         bool
	lines                 []string
	partial               string
	duration              float64
	started               time.Time
	encodingStarted       time.Time
	scroll                int
}

func New(in *os.File, out io.Writer) *Dashboard {
	d := &Dashboard{
		in:         in,
		out:        out,
		stop:       make(chan struct{}),
		quit:       make(chan struct{}),
		workerByID: make(map[string]*workerState),
	}
	if f, ok := out.(*os.File); ok {
		d.interactive = term.IsTerminal(int(f.Fd())) && in != nil && term.IsTerminal(int(in.Fd()))
	}
	return d
}

func (d *Dashboard) Start() {
	if !d.interactive {
		return
	}
	if state, err := term.MakeRaw(int(d.in.Fd())); err == nil {
		d.oldState = state
	} else {
		d.interactive = false
		return
	}
	fmt.Fprint(d.out, "\x1b[?1049h\x1b[?25l")
	go d.readKeys()
	d.renderWG.Add(1)
	go d.renderLoop()
}

func (d *Dashboard) Close() {
	d.stopOnce.Do(func() { close(d.stop) })
	d.renderWG.Wait()
	if !d.interactive {
		return
	}
	d.render()
	fmt.Fprint(d.out, "\x1b[?25h\x1b[?1049l")
	if d.oldState != nil {
		_ = term.Restore(int(d.in.Fd()), d.oldState)
	}
}

func (d *Dashboard) Quit() <-chan struct{} { return d.quit }

func (d *Dashboard) Interactive() bool { return d.interactive }

func (d *Dashboard) SetTotal(total int) {
	d.mu.Lock()
	d.total = total
	d.mu.Unlock()
}

func (d *Dashboard) Event(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	parts := splitDisplayLines(message)
	timestamp := time.Now().Format("15:04:05") + "  "
	d.mu.Lock()
	for i, part := range parts {
		prefix := strings.Repeat(" ", len(timestamp))
		if i == 0 {
			prefix = timestamp
		}
		d.events = appendLimited(d.events, prefix+part)
	}
	d.mu.Unlock()
	if !d.interactive {
		fmt.Fprintln(d.out, message)
	}
}

func (d *Dashboard) RegisterWorker(id, node string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.workerByID[id]; ok {
		return
	}
	w := &workerState{id: id, node: node, status: "idle"}
	d.workerByID[id] = w
	d.workers = append(d.workers, w)
}

func (d *Dashboard) JobStarted(id, path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	w := d.ensureWorker(id)
	w.job, w.status, w.progress = filepath.Base(path), "working", 0
	w.progressKnown = false
	w.lines, w.partial, w.duration = nil, "", 0
	w.started, w.encodingStarted = time.Now(), time.Time{}
	w.scroll = 0
}

func (d *Dashboard) JobStage(id, stage string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	w := d.ensureWorker(id)
	w.status = stage
	if stage == "encoding" && w.encodingStarted.IsZero() {
		w.encodingStarted = time.Now()
	}
}

func (d *Dashboard) JobFinished(id string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	w := d.ensureWorker(id)
	if err != nil {
		d.failed++
		w.status = "failed"
	} else {
		d.done++
		w.status, w.progress = "done", 100
		w.progressKnown = true
	}
}

func (d *Dashboard) WorkerWriter(id string) io.Writer {
	return &workerWriter{dashboard: d, id: id}
}

func (d *Dashboard) ensureWorker(id string) *workerState {
	if w := d.workerByID[id]; w != nil {
		return w
	}
	w := &workerState{id: id, node: id, status: "idle"}
	d.workerByID[id] = w
	d.workers = append(d.workers, w)
	return w
}

func (d *Dashboard) renderLoop() {
	defer d.renderWG.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-ticker.C:
			d.render()
		}
	}
}

func (d *Dashboard) readKeys() {
	buf := make([]byte, 16)
	for {
		n, err := d.in.Read(buf)
		if err != nil {
			return
		}
		d.mu.Lock()
		key := string(buf[:n])
		switch {
		case key == "q" || key == "\x03":
			d.quitOnce.Do(func() { close(d.quit) })
		case key == "j" || key == "\t":
			d.selectDelta(1)
		case key == "k":
			d.selectDelta(-1)
		case key == "\x1b[B":
			d.selectDelta(1)
		case key == "\x1b[A":
			d.selectDelta(-1)
		case key == "\x1b[5~" || key == "u":
			d.scrollSelected(max(1, d.outputRows-1))
		case key == "\x1b[6~" || key == "d":
			d.scrollSelected(-max(1, d.outputRows-1))
		case key == "\x1b[H" || key == "\x1b[1~" || key == "g":
			d.scrollSelectedToStart()
		case key == "\x1b[F" || key == "\x1b[4~" || key == "G":
			d.scrollSelectedToEnd()
		case n == 1 && buf[0] >= '1' && buf[0] <= '9':
			idx := int(buf[0] - '1')
			if idx < len(d.workers) {
				d.selected = idx
			}
		}
		d.mu.Unlock()
	}
}

func (d *Dashboard) scrollSelected(delta int) {
	if len(d.workers) == 0 {
		return
	}
	w := d.workers[d.selected]
	w.scroll = min(max(0, w.scroll+delta), d.maxScroll(w))
}

func (d *Dashboard) scrollSelectedToStart() {
	if len(d.workers) > 0 {
		w := d.workers[d.selected]
		w.scroll = d.maxScroll(w)
	}
}

func (d *Dashboard) maxScroll(w *workerState) int {
	return max(0, len(w.lines)-max(1, d.outputRows))
}

func (d *Dashboard) scrollSelectedToEnd() {
	if len(d.workers) > 0 {
		d.workers[d.selected].scroll = 0
	}
}

func (d *Dashboard) selectDelta(delta int) {
	if len(d.workers) == 0 {
		return
	}
	d.selected = (d.selected + delta + len(d.workers)) % len(d.workers)
}

func (d *Dashboard) render() {
	if !d.interactive {
		return
	}
	width, height, err := term.GetSize(int(d.in.Fd()))
	if err != nil || width < 40 || height < 14 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	var b strings.Builder
	title := fmt.Sprintf("\x1b[1;36m TELECONVERT \x1b[0m  %s  %d/%d done  %d failed",
		bar(d.done+d.failed, d.total, min(24, width/4)), d.done, d.total, d.failed)
	writeRow(&b, title, width)

	// Three bordered sections consume six rows; reserve one row each for the
	// header and footer, then divide the remaining content area.
	contentRows := height - 8
	workerRows := min(max(2, len(d.workers)), max(2, contentRows/3))
	remaining := contentRows - workerRows
	eventRows := max(2, remaining/2)
	outputRows := max(2, remaining-eventRows)
	d.outputRows = outputRows

	writePanelTop(&b, "Workers (↑/↓, j/k, tab or 1-9 to select)", width)
	for i := 0; i < workerRows; i++ {
		if i >= len(d.workers) {
			writePanelRow(&b, "", width)
			continue
		}
		w := d.workers[i]
		marker := " "
		if i == d.selected {
			marker = "\x1b[7m>\x1b[0m"
		}
		barWidth := min(18, width/5)
		progressBar := activityBar(barWidth, time.Now())
		progressText := " --.-%"
		timing := elapsedLabel(w)
		if w.progressKnown {
			progressBar = bar(int(w.progress), 100, barWidth)
			progressText = fmt.Sprintf("%5.1f%%", w.progress)
			if eta := etaLabel(w); eta != "" {
				timing += " ETA " + eta
			}
		}
		row := fmt.Sprintf("%s %-14s %-10s %s %s  %-18s %s",
			marker, w.id, w.status, progressBar, progressText, timing, w.job)
		writePanelRow(&b, row, width)
	}
	writePanelBottom(&b, width)

	writePanelTop(&b, "Teleconvert output", width)
	writePanelTail(&b, wrapLogLines(d.events, width-2), eventRows, width)
	writePanelBottom(&b, width)

	selectedName := "none"
	var output []string
	if len(d.workers) > 0 {
		if d.selected >= len(d.workers) {
			d.selected = len(d.workers) - 1
		}
		selectedName = d.workers[d.selected].id
		selectedWorker := d.workers[d.selected]
		output = scrolledWindow(selectedWorker.lines, outputRows, selectedWorker.scroll)
		if selectedWorker.scroll > 0 {
			selectedName += fmt.Sprintf("  [scrollback: %d lines from live]", selectedWorker.scroll)
		} else {
			selectedName += "  [live]"
		}
	}
	writePanelTop(&b, "Encoder output — "+selectedName, width)
	writePanelTail(&b, output, outputRows, width)
	writePanelBottom(&b, width)
	writeRow(&b, "\x1b[2m PgUp/PgDn or u/d: scroll • g/G: oldest/live • q/Ctrl-C: clean shutdown\x1b[0m", width)
	d.writeChangedRows(b.String(), width, height)
}

// writeChangedRows avoids repainting the whole terminal for every progress
// tick. Synchronized output lets supporting terminals present a multi-row
// update atomically; terminals that do not support it safely ignore the mode.
func (d *Dashboard) writeChangedRows(frame string, width, height int) {
	frame = strings.TrimSuffix(frame, "\r\n")
	rows := strings.Split(frame, "\r\n")
	resized := width != d.lastWidth || height != d.lastHeight

	var update strings.Builder
	update.WriteString("\x1b[?2026h")
	if resized {
		update.WriteString("\x1b[2J")
		d.lastFrame = nil
	}
	for i, row := range rows {
		if i < len(d.lastFrame) && row == d.lastFrame[i] {
			continue
		}
		fmt.Fprintf(&update, "\x1b[%d;1H%s", i+1, row)
	}
	// Clear rows left behind if a resize or layout change shortened the frame.
	for i := len(rows); i < len(d.lastFrame); i++ {
		fmt.Fprintf(&update, "\x1b[%d;1H\x1b[2K", i+1)
	}
	update.WriteString("\x1b[?2026l")

	// The begin/end markers alone need not be written when nothing changed.
	if resized || len(update.String()) > len("\x1b[?2026h\x1b[?2026l") {
		fmt.Fprint(d.out, update.String())
	}
	d.lastFrame = append(d.lastFrame[:0], rows...)
	d.lastWidth, d.lastHeight = width, height
}

type workerWriter struct {
	dashboard *Dashboard
	id        string
}

func (w *workerWriter) Write(p []byte) (int, error) {
	d := w.dashboard
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.interactive {
		if _, err := fmt.Fprintf(d.out, "[%s] %s", w.id, p); err != nil {
			return 0, err
		}
	}
	state := d.ensureWorker(w.id)
	text := state.partial + strings.ReplaceAll(string(p), "\r", "\n")
	parts := strings.Split(text, "\n")
	state.partial = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		line = stripANSI(strings.TrimSpace(line))
		if line == "" {
			continue
		}
		if !updateProgress(state, line) {
			state.lines = appendLimited(state.lines, line)
			if state.scroll > 0 {
				state.scroll = min(state.scroll+1, d.maxScroll(state))
			}
		}
	}
	return len(p), nil
}

func scrolledWindow(lines []string, rows, scroll int) []string {
	end := min(len(lines), max(0, len(lines)-scroll))
	start := max(0, end-rows)
	return lines[start:end]
}

func wrapLogLines(lines []string, width int) []string {
	if width <= 0 {
		return nil
	}
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		parts := wrapLine(line, width)
		wrapped = append(wrapped, parts...)
	}
	return wrapped
}

func wrapLine(line string, width int) []string {
	runes := []rune(line)
	if len(runes) == 0 {
		return []string{""}
	}
	const continuationIndent = "          "
	var result []string
	first := true
	for len(runes) > 0 {
		indent := ""
		available := width
		if !first && width > len(continuationIndent)+8 {
			indent = continuationIndent
			available -= len(continuationIndent)
		}
		if len(runes) <= available {
			result = append(result, indent+string(runes))
			break
		}
		cut := available
		for i := available; i > available/2; i-- {
			if runes[i-1] == ' ' {
				cut = i - 1
				break
			}
		}
		result = append(result, indent+string(runes[:cut]))
		runes = []rune(strings.TrimLeft(string(runes[cut:]), " "))
		first = false
	}
	return result
}

// updateProgress returns true for transient encoder status lines. Callers use
// this to update the progress bar without flooding the persistent output pane.
func updateProgress(w *workerState, line string) bool {
	transient := false
	if m := handBrakeTask.FindStringSubmatch(line); len(m) == 4 {
		transient = true
		task, taskErr := strconv.Atoi(m[1])
		total, totalErr := strconv.Atoi(m[2])
		p, percentErr := strconv.ParseFloat(m[3], 64)
		if taskErr == nil && totalErr == nil && percentErr == nil && total > 0 {
			w.progress = clamp((float64(task-1) + p/100) / float64(total) * 100)
			w.progressKnown = true
		}
	} else if m := plainPercent.FindStringSubmatch(line); len(m) == 2 &&
		strings.Contains(strings.ToLower(line), "encoding") {
		transient = true
		if p, err := strconv.ParseFloat(m[1], 64); err == nil {
			w.progress = clamp(p)
			w.progressKnown = true
		}
	}
	if m := ffmpegDuration.FindStringSubmatch(line); len(m) == 4 {
		w.duration = clockSeconds(m[1], m[2], m[3])
	}
	if w.duration > 0 {
		if m := ffmpegTime.FindStringSubmatch(line); len(m) == 4 {
			transient = true
			w.progress = clamp(clockSeconds(m[1], m[2], m[3]) / w.duration * 100)
			w.progressKnown = true
		} else if m := ffmpegOutTime.FindStringSubmatch(line); len(m) == 2 {
			transient = true
			if micros, err := strconv.ParseFloat(m[1], 64); err == nil {
				w.progress = clamp((micros / 1_000_000) / w.duration * 100)
				w.progressKnown = true
			}
		}
	}
	trimmed := strings.TrimSpace(line)
	return transient || handBrakeStatus.MatchString(trimmed) || ffmpegProgress.MatchString(trimmed)
}

func elapsedLabel(w *workerState) string {
	start := w.started
	if !w.encodingStarted.IsZero() {
		start = w.encodingStarted
	}
	if start.IsZero() {
		return ""
	}
	return "elapsed " + formatDuration(time.Since(start))
}

func etaLabel(w *workerState) string {
	if !w.progressKnown || w.progress <= 0 || w.encodingStarted.IsZero() {
		return ""
	}
	elapsed := time.Since(w.encodingStarted)
	remaining := time.Duration(float64(elapsed) * (100 - w.progress) / w.progress)
	return formatDuration(remaining)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h := int(d / time.Hour)
	m := int(d%time.Hour) / int(time.Minute)
	s := int(d%time.Minute) / int(time.Second)
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func clockSeconds(h, m, s string) float64 {
	hh, _ := strconv.ParseFloat(h, 64)
	mm, _ := strconv.ParseFloat(m, 64)
	ss, _ := strconv.ParseFloat(s, 64)
	return hh*3600 + mm*60 + ss
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func appendLimited(lines []string, line string) []string {
	lines = append(lines, line)
	if len(lines) > maxLogLines {
		copy(lines, lines[len(lines)-maxLogLines:])
		lines = lines[:maxLogLines]
	}
	return lines
}

func writePanelTail(b *strings.Builder, lines []string, rows, width int) {
	start := max(0, len(lines)-rows)
	for _, line := range lines[start:] {
		writePanelRow(b, line, width)
	}
	for i := len(lines) - start; i < rows; i++ {
		writePanelRow(b, "", width)
	}
}

func writePanelTop(b *strings.Builder, title string, width int) {
	available := max(1, width-4)
	if visibleWidth(title) > available {
		title = truncateANSIUnsafe(title, available-1) + "…"
	}
	label := " " + title + " "
	writeRow(b, "┌"+label+strings.Repeat("─", max(0, width-2-visibleWidth(label)))+"┐", width)
}

func writePanelRow(b *strings.Builder, s string, width int) {
	s = sanitizeDisplayLine(s)
	innerWidth := width - 2
	if visibleWidth(s) > innerWidth {
		s = truncateANSIUnsafe(s, innerWidth-1) + "…"
	}
	padding := max(0, innerWidth-visibleWidth(s))
	writeRow(b, "│"+s+strings.Repeat(" ", padding)+"│", width)
}

func writePanelBottom(b *strings.Builder, width int) {
	writeRow(b, "└"+strings.Repeat("─", width-2)+"┘", width)
}

func writeRow(b *strings.Builder, s string, width int) {
	if visibleWidth(s) > width {
		s = truncateANSIUnsafe(s, width-1) + "…"
	}
	b.WriteString(s)
	b.WriteString("\x1b[K\r\n")
}

func visibleWidth(s string) int { return len([]rune(stripANSI(s))) }

func splitDisplayLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	parts := strings.Split(s, "\n")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return []string{""}
	}
	for i := range parts {
		parts[i] = stripANSI(sanitizeDisplayLine(parts[i]))
	}
	return parts
}

func sanitizeDisplayLine(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r == '\x1b':
			return r
		case r < 0x20 || r == 0x7f:
			return -1
		default:
			return r
		}
	}, s)
}

func bar(value, total, width int) string {
	if width < 3 {
		return ""
	}
	n := 0
	if total > 0 {
		n = value * width / total
	}
	n = min(max(n, 0), width)
	return "[" + strings.Repeat("=", n) + strings.Repeat(" ", width-n) + "]"
}

func activityBar(width int, now time.Time) string {
	if width < 3 {
		return ""
	}
	const pulseWidth = 3
	inner := make([]rune, width)
	for i := range inner {
		inner[i] = ' '
	}
	span := max(1, width-pulseWidth+1)
	cycle := max(1, span*2-1)
	pos := int(now.UnixMilli()/150) % cycle
	if pos >= span {
		pos = span*2 - 2 - pos
	}
	for i := pos; i < min(width, pos+pulseWidth); i++ {
		inner[i] = '='
	}
	return "[" + string(inner) + "]"
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func stripANSI(s string) string { return ansi.ReplaceAllString(s, "") }

// Styled rows are short fixed labels; truncating bytes is acceptable after
// stripping styles from long dynamic rows.
func truncateANSIUnsafe(s string, width int) string {
	if strings.Contains(s, "\x1b") {
		s = stripANSI(s)
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width])
}
