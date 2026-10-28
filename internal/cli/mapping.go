package cli

import (
	"cmp"
	"os"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/codemap"
	"github.com/shhac/agent-figma/internal/output"
	"github.com/shhac/agent-figma/internal/simplify"
)

// mapping is the repository's component-to-code mapping, when it has one.
type mapping struct {
	path   string
	exists bool
	file   *codemap.File
}

// loadMapping reads the mapping for a map command, where a broken file is
// the problem to report.
func loadMapping(globals *shared.GlobalFlags) (mapping, error) {
	wd, _ := os.Getwd()
	path, exists := codemap.Locate(globals.ComponentsFile, wd)
	f, err := codemap.Load(path)
	if err != nil {
		return mapping{}, agenterrors.Wrap(err, agenterrors.FixableByAgent).WithHint("Fix or move " + path + "; 'agent-figma components map --help' describes the format.")
	}
	return mapping{path: path, exists: exists, file: f}, nil
}

// readMapping reads the mapping for a read command, which should still
// answer when the mapping is broken: it notes the problem and goes unmapped.
func readMapping(globals *shared.GlobalFlags) mapping {
	m, err := loadMapping(globals)
	if err != nil {
		output.Notice("Ignoring the component mapping: "+err.Error(), "Output is unmapped until the file is fixed.")
		return mapping{}
	}
	return m
}

// refuseMappingWrite keeps MCP clients from writing into the server's
// filesystem, as --out is refused there.
func refuseMappingWrite() error {
	if os.Getenv(ViaMCPEnv) == "" {
		return nil
	}
	return agenterrors.New("changing the component mapping is not available through the MCP server", agenterrors.FixableByHuman).
		WithHint("Run 'agent-figma components map add' (or suggest --write) in a terminal in the repository.")
}

// annotate adds the mapped code component to @components entries and
// instance records. Without a mapping file nothing is added, so output stays
// lean until a repository opts in.
func (m mapping) annotate(result *simplify.Result, store *cache.NodeStore) {
	if !m.exists {
		return
	}
	m.annotateUses(result.Components)
	m.annotateRecords(result.Records, store)
}

// annotateUses marks each component use mapped or not, with its code.
func (m mapping) annotateUses(uses []simplify.ComponentUse) {
	for i := range uses {
		u := &uses[i]
		entry := m.file.Lookup(u.SetKey, u.Key, cmp.Or(u.Set, u.Component))
		mapped := entry != nil
		u.Mapped = &mapped
		if !mapped {
			continue
		}
		u.Code = codeRef(entry)
		for j := range u.Variants {
			u.Variants[j].CodeProps = entry.CodeProps(u.Variants[j].Props)
		}
	}
}

// annotateRecords adds the mapped code component to instance records.
func (m mapping) annotateRecords(records []simplify.Record, store *cache.NodeStore) {
	for i := range records {
		rec := &records[i]
		if rec.Component == nil {
			continue
		}
		c := store.Components[rec.Component.ID]
		if entry := m.file.Lookup(store.ComponentSets[c.ComponentSetID].Key, c.Key, cmp.Or(rec.Component.Set, rec.Component.Name)); entry != nil {
			code := codeRef(entry)
			code.Props = entry.CodeProps(rec.Props)
			rec.Code = code
		}
	}
}

func codeRef(e *codemap.Entry) *simplify.CodeRef {
	return &simplify.CodeRef{Component: e.Code.Component, Import: e.Code.Import, Notes: e.Notes}
}
