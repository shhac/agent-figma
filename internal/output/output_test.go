package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestStreamNDJSONKeepsOrder(t *testing.T) {
	var buf bytes.Buffer
	defer SetWritersForTest(&buf, nil)()
	s, err := NewStream("")
	if err != nil {
		t.Fatal(err)
	}
	s.Meta(MetaFile, map[string]any{"key": "K"})
	s.Item(map[string]any{"id": "1:2"})
	s.MetaEach(MetaRelated, map[string]any{"id": "1:3"})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], `{"@file"`) || !strings.HasPrefix(lines[2], `{"@related"`) {
		t.Fatalf("unexpected lines: %q", lines)
	}
}

func TestStreamJSONEnvelope(t *testing.T) {
	var buf bytes.Buffer
	defer SetWritersForTest(&buf, nil)()
	s, _ := NewStream("json")
	s.Item(map[string]any{"id": "1:2"})
	s.MetaEach(MetaRelated, map[string]any{"id": "1:3"})
	s.MetaEach(MetaRelated, map[string]any{"id": "1:4"})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env["data"].([]any)) != 1 || len(env[MetaRelated].([]any)) != 2 {
		t.Fatalf("unexpected envelope: %v", env)
	}
}
