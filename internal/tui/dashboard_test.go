package tui

import (
	"bytes"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWorkerWriterParsesHandBrakeProgress(t *testing.T) {
	d := New(nil, &bytes.Buffer{})
	w := d.WorkerWriter("node#0")
	_, _ = w.Write([]byte("Encoding: task 1 of 1, 42.37 % (12.00 fps)\r"))

	got := d.workerByID["node#0"].progress
	if math.Abs(got-42.37) > 0.001 {
		t.Fatalf("progress = %v, want 42.37", got)
	}
	if !d.workerByID["node#0"].progressKnown {
		t.Fatal("HandBrake progress should be measurable")
	}
}

func TestWorkerWriterParsesFFmpegProgress(t *testing.T) {
	d := New(nil, &bytes.Buffer{})
	w := d.WorkerWriter("node#0")
	_, _ = w.Write([]byte("Duration: 00:02:00.00, start: 0.000\n"))
	_, _ = w.Write([]byte("frame=123 time=00:00:30.00 speed=2x\r"))

	got := d.workerByID["node#0"].progress
	if math.Abs(got-25) > 0.001 {
		t.Fatalf("progress = %v, want 25", got)
	}
}

func TestHandBrakeMultiPassProgressDoesNotReset(t *testing.T) {
	d := New(nil, &bytes.Buffer{})
	w := d.WorkerWriter("node#0")
	_, _ = w.Write([]byte("Encoding: task 2 of 2, 10.00 %\r"))

	got := d.workerByID["node#0"].progress
	if math.Abs(got-55) > 0.001 {
		t.Fatalf("progress = %v, want 55", got)
	}
}

func TestFFmpegProgressAllowsWhitespaceAfterTime(t *testing.T) {
	d := New(nil, &bytes.Buffer{})
	w := d.WorkerWriter("node#0")
	_, _ = w.Write([]byte("Duration: 00:10:00.00\n"))
	_, _ = w.Write([]byte("frame=2 time= 00:02:30.00 speed=1x\r"))

	if got := d.workerByID["node#0"].progress; math.Abs(got-25) > 0.001 {
		t.Fatalf("progress = %v, want 25", got)
	}
}

func TestActivityBarMovesWhenProgressIsUnknown(t *testing.T) {
	first := activityBar(12, time.UnixMilli(0))
	second := activityBar(12, time.UnixMilli(300))
	if first == second {
		t.Fatalf("activity bar did not move: %q", first)
	}
}

func TestWorkerOutputIsSeparatedBySlot(t *testing.T) {
	d := New(nil, &bytes.Buffer{})
	_, _ = d.WorkerWriter("node#0").Write([]byte("first\n"))
	_, _ = d.WorkerWriter("node#1").Write([]byte("second\n"))

	if got := d.workerByID["node#0"].lines; len(got) != 1 || got[0] != "first" {
		t.Fatalf("node#0 lines = %#v", got)
	}
	if got := d.workerByID["node#1"].lines; len(got) != 1 || got[0] != "second" {
		t.Fatalf("node#1 lines = %#v", got)
	}
}

func TestProgressLinesDoNotFloodWorkerOutput(t *testing.T) {
	d := New(nil, &bytes.Buffer{})
	w := d.WorkerWriter("node#0")
	_, _ = w.Write([]byte("Duration: 00:02:00.00\n"))
	for i := 1; i <= 20; i++ {
		_, _ = w.Write([]byte(fmt.Sprintf("frame=%d time=00:00:%02d.00 speed=1x\r", i, i)))
	}
	_, _ = w.Write([]byte("warning: encoder diagnostic\n"))

	lines := d.workerByID["node#0"].lines
	if len(lines) != 2 {
		t.Fatalf("persistent output has %d lines, want duration and diagnostic: %#v", len(lines), lines)
	}
	if lines[1] != "warning: encoder diagnostic" {
		t.Fatalf("diagnostic line was not preserved: %#v", lines)
	}
	if !d.workerByID["node#0"].progressKnown {
		t.Fatal("filtered progress lines must still update the progress bar")
	}
}

func TestHandBrakeProgressLinesAreFiltered(t *testing.T) {
	d := New(nil, &bytes.Buffer{})
	w := d.WorkerWriter("node#0")
	_, _ = w.Write([]byte("Encoding: task 1 of 1, 12.00 %\r"))
	_, _ = w.Write([]byte("Encoding: task 1 of 1, 13.00 %\r"))
	_, _ = w.Write([]byte("libhb: useful diagnostic\n"))

	lines := d.workerByID["node#0"].lines
	if len(lines) != 1 || lines[0] != "libhb: useful diagnostic" {
		t.Fatalf("progress lines leaked into persistent output: %#v", lines)
	}
}

func TestNonInteractiveWorkerOutputIsPreserved(t *testing.T) {
	var out bytes.Buffer
	d := New(nil, &out)
	_, err := d.WorkerWriter("node#0").Write([]byte("encoder detail\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "[node#0] encoder detail\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestPanelHasClearBorderAndFixedWidth(t *testing.T) {
	var out strings.Builder
	writePanelTop(&out, "Workers", 24)
	writePanelRow(&out, "node#0", 24)
	writePanelBottom(&out, 24)

	lines := strings.Split(strings.TrimSuffix(out.String(), "\r\n"), "\r\n")
	if len(lines) != 3 {
		t.Fatalf("panel has %d lines, want 3", len(lines))
	}
	if !strings.HasPrefix(lines[0], "┌ Workers ") || !strings.HasSuffix(lines[0], "┐\x1b[K") {
		t.Fatalf("unexpected top border: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "│node#0") || !strings.Contains(lines[2], "└") {
		t.Fatalf("panel border missing: %#v", lines)
	}
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\x1b[K")
		if got := visibleWidth(line); got != 24 {
			t.Fatalf("line width = %d, want 24: %q", got, line)
		}
	}
}

func TestDifferentialRendererSkipsUnchangedRows(t *testing.T) {
	var out bytes.Buffer
	d := New(nil, &out)
	frame := "first\x1b[K\r\nsecond\x1b[K\r\n"

	d.writeChangedRows(frame, 80, 24)
	if out.Len() == 0 {
		t.Fatal("initial frame was not written")
	}
	out.Reset()
	d.writeChangedRows(frame, 80, 24)
	if out.Len() != 0 {
		t.Fatalf("unchanged frame produced %d bytes: %q", out.Len(), out.String())
	}
}

func TestDifferentialRendererWritesOnlyChangedRow(t *testing.T) {
	var out bytes.Buffer
	d := New(nil, &out)
	d.writeChangedRows("first\x1b[K\r\nsecond\x1b[K\r\n", 80, 24)
	out.Reset()

	d.writeChangedRows("first\x1b[K\r\nchanged\x1b[K\r\n", 80, 24)
	got := out.String()
	if strings.Contains(got, "first") {
		t.Fatalf("unchanged row was repainted: %q", got)
	}
	if !strings.Contains(got, "\x1b[2;1Hchanged") {
		t.Fatalf("changed second row was not addressed directly: %q", got)
	}
}

func TestEncoderOutputScrollback(t *testing.T) {
	d := New(nil, &bytes.Buffer{})
	d.outputRows = 3
	w := &workerState{lines: []string{"one", "two", "three", "four", "five"}}
	d.workers = []*workerState{w}

	d.scrollSelected(2)
	got := scrolledWindow(w.lines, d.outputRows, w.scroll)
	want := []string{"one", "two", "three"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scrolled window = %#v, want %#v", got, want)
	}
	d.scrollSelectedToEnd()
	got = scrolledWindow(w.lines, d.outputRows, w.scroll)
	want = []string{"three", "four", "five"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("live window = %#v, want %#v", got, want)
	}
}

func TestScrollbackStaysAnchoredWhenOutputArrives(t *testing.T) {
	d := New(nil, &bytes.Buffer{})
	d.outputRows = 2
	d.RegisterWorker("node#0", "node")
	w := d.workerByID["node#0"]
	w.lines = []string{"one", "two", "three", "four"}
	w.scroll = 2

	_, _ = d.WorkerWriter("node#0").Write([]byte("five\n"))
	got := scrolledWindow(w.lines, d.outputRows, w.scroll)
	if want := []string{"one", "two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scrollback moved after new output: got %#v, want %#v", got, want)
	}
}

func TestTeleconvertOutputWrapsLongMessages(t *testing.T) {
	lines := []string{
		"12:34:56  Done /a/very/long/source/directory/video.mp4 -> /another/long/output/video.mp4",
	}
	wrapped := wrapLogLines(lines, 36)
	if len(wrapped) < 3 {
		t.Fatalf("long event was not wrapped: %#v", wrapped)
	}
	for _, line := range wrapped {
		if visibleWidth(line) > 36 {
			t.Fatalf("wrapped event exceeds pane width: %q", line)
		}
	}
	joined := strings.ReplaceAll(strings.Join(wrapped, ""), "          ", "")
	for _, fragment := range []string{"12:34:56", "source/directory", "output/video.mp4"} {
		if !strings.Contains(joined, fragment) {
			t.Fatalf("wrapped event lost %q: %#v", fragment, wrapped)
		}
	}
}

func TestTeleconvertOutputWrapsUnbrokenPath(t *testing.T) {
	path := "/this/is/an/extremely/long/path/without/any/spaces/video-file.mp4"
	wrapped := wrapLine(path, 20)
	if len(wrapped) < 2 {
		t.Fatalf("path was not wrapped: %#v", wrapped)
	}
	for _, line := range wrapped {
		if visibleWidth(line) > 20 {
			t.Fatalf("wrapped path exceeds width: %q", line)
		}
	}
}

func TestTeleconvertMultilineEventCannotEscapePanel(t *testing.T) {
	d := New(nil, &bytes.Buffer{})
	d.Event("remote error:\rfirst line\nsecond line\x1b[31m")

	if len(d.events) != 3 {
		t.Fatalf("multiline event produced %d rows, want 3: %#v", len(d.events), d.events)
	}
	for _, event := range d.events {
		if strings.ContainsAny(event, "\r\n") {
			t.Fatalf("event retained cursor-moving character: %q", event)
		}
		if strings.Contains(event, "\x1b") {
			t.Fatalf("event retained terminal escape sequence: %q", event)
		}
	}
	if !strings.HasPrefix(d.events[1], "          ") {
		t.Fatalf("continuation row is not aligned under timestamp: %q", d.events[1])
	}
}

func TestPanelRowRemovesEmbeddedNewlines(t *testing.T) {
	var out strings.Builder
	writePanelRow(&out, "before\r\nafter", 24)
	got := strings.TrimSuffix(out.String(), "\r\n")
	if strings.Count(got, "\n") != 0 || strings.Count(got, "\r") != 0 {
		t.Fatalf("panel content moved the terminal cursor: %q", got)
	}
	if !strings.Contains(got, "beforeafter") {
		t.Fatalf("panel content was not preserved: %q", got)
	}
}
