// Package output is agent-figma's output policy on top of lib-agent-output,
// which owns the wire mechanism: the writer indirection tests capture, and a
// Stream that writes records and @-prefixed metadata as NDJSON lines, or
// gathers them into one envelope for --format json/yaml.
package output

import (
	"io"
	"os"
	"sync"

	out "github.com/shhac/lib-agent-output"

	// Registers the shared YAML encoder for --format yaml.
	_ "github.com/shhac/lib-agent-cli/yaml"
)

type Format = out.Format

const (
	FormatJSON   = out.FormatJSON
	FormatYAML   = out.FormatYAML
	FormatNDJSON = out.FormatNDJSON
)

// Metadata line keys shared across commands.
const (
	MetaFile       = "@file"
	MetaStyles     = "@styles"
	MetaRelated    = "@related"
	MetaTruncated  = "@truncated"
	MetaBudget     = "@budget"
	MetaUnresolved = "@unresolved"
	MetaPagination = out.MetaKeyPagination
)

var (
	writersMu sync.RWMutex
	stdout    io.Writer = os.Stdout
	stderr    io.Writer = os.Stderr
)

func Stdout() io.Writer {
	writersMu.RLock()
	defer writersMu.RUnlock()
	return stdout
}

func Stderr() io.Writer {
	writersMu.RLock()
	defer writersMu.RUnlock()
	return stderr
}

// SetWritersForTest redirects output and returns a function restoring it.
func SetWritersForTest(o, e io.Writer) func() {
	writersMu.Lock()
	prevOut, prevErr := stdout, stderr
	if o != nil {
		stdout = o
	}
	if e != nil {
		stderr = e
	}
	writersMu.Unlock()
	return func() {
		writersMu.Lock()
		stdout, stderr = prevOut, prevErr
		writersMu.Unlock()
	}
}

// WriteItem prints one record in the requested format.
func WriteItem(item any, format string) error {
	f, err := out.ResolveFormat(format, FormatNDJSON)
	if err != nil {
		return err
	}
	if f == FormatNDJSON {
		return out.NewNDJSONWriter(Stdout()).WriteItem(item)
	}
	return out.Print(Stdout(), item, f, nil)
}

// WriteList prints records plus single-valued metadata.
func WriteList(items []any, meta map[string]any, format string) error {
	f, err := out.ResolveFormat(format, FormatNDJSON)
	if err != nil {
		return err
	}
	if items == nil {
		items = []any{}
	}
	return out.WriteList(Stdout(), f, items, meta, nil)
}

// Notice writes a non-fatal {notice, hint} line to stderr.
func Notice(notice, hint string) {
	out.WriteNotice(Stderr(), notice, hint)
}

// Stream writes records in order. As NDJSON every record and metadata line is
// written as it arrives, so an agent can read the head of a long result; for
// json/yaml everything is gathered and printed as one envelope on Close.
type Stream struct {
	format Format
	nd     *out.NDJSONWriter
	items  []any
	meta   map[string]any
	err    error
}

func NewStream(format string) (*Stream, error) {
	f, err := out.ResolveFormat(format, FormatNDJSON)
	if err != nil {
		return nil, err
	}
	s := &Stream{format: f, meta: map[string]any{}}
	if f == FormatNDJSON {
		s.nd = out.NewNDJSONWriter(Stdout())
	}
	return s, nil
}

// Item writes one data record.
func (s *Stream) Item(item any) {
	if s.err != nil {
		return
	}
	if s.nd != nil {
		s.err = s.nd.WriteItem(item)
		return
	}
	s.items = append(s.items, item)
}

// Meta writes a metadata value that occurs once per result (@file, @budget).
func (s *Stream) Meta(key string, value any) {
	if s.err != nil {
		return
	}
	if s.nd != nil {
		s.err = s.nd.WriteMetaLine(key, value)
		return
	}
	s.meta[key] = value
}

// MetaEach writes a metadata value that can repeat (@related, @unresolved);
// in an envelope the values are gathered into a list under the key.
func (s *Stream) MetaEach(key string, value any) {
	if s.err != nil {
		return
	}
	if s.nd != nil {
		s.err = s.nd.WriteMetaLine(key, value)
		return
	}
	list, _ := s.meta[key].([]any)
	s.meta[key] = append(list, value)
}

// Close flushes an envelope and reports the first write error.
func (s *Stream) Close() error {
	if s.err != nil || s.nd != nil {
		return s.err
	}
	items := s.items
	if items == nil {
		items = []any{}
	}
	return out.WriteList(Stdout(), s.format, items, s.meta, nil)
}
