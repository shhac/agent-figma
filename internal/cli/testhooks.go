package cli

import (
	"io"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/output"
)

// emptyReader is the stdin RunForTest gives commands, so a token read from a
// pipe finds nothing rather than the test runner's own stdin.
type emptyReader struct{}

func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

func writeError(err error) { agenterrors.WriteError(output.Stderr(), err) }
