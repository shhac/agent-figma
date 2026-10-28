package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestUsagePrintsReferenceCard(t *testing.T) {
	root := newRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"usage"})
	if err := root.Execute(); err != nil {
		t.Fatalf("usage: %v", err)
	}
	if !strings.HasPrefix(out.String(), "agent-figma") {
		t.Fatalf("usage output %q missing reference card header", out.String())
	}
}

func TestVersionFlag(t *testing.T) {
	root := newRootCmd("1.2.3")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--version: %v", err)
	}
	if !strings.Contains(out.String(), "1.2.3") {
		t.Fatalf("version output %q missing version", out.String())
	}
}
