package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/api"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/figma"
	"github.com/shhac/agent-figma/internal/output"
	"github.com/shhac/agent-figma/internal/simplify"
)

type variableCollection struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Key                  string `json:"key"`
	DefaultModeID        string `json:"defaultModeId"`
	Remote               bool   `json:"remote"`
	HiddenFromPublishing bool   `json:"hiddenFromPublishing"`
	Modes                []struct {
		ModeID string `json:"modeId"`
		Name   string `json:"name"`
	} `json:"modes"`
}

type variable struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Key                  string `json:"key"`
	VariableCollectionID string `json:"variableCollectionId"`
	ResolvedType         string `json:"resolvedType"`
	// ResolvedDataType is the published-variables spelling of ResolvedType.
	ResolvedDataType string                     `json:"resolvedDataType"`
	Description      string                     `json:"description"`
	Scopes           []string                   `json:"scopes"`
	Remote           bool                       `json:"remote"`
	CodeSyntax       map[string]string          `json:"codeSyntax"`
	ValuesByMode     map[string]json.RawMessage `json:"valuesByMode"`
}

func registerVariables(root *cobra.Command, globals *shared.GlobalFlags) {
	var published bool
	group := &cobra.Command{Use: "variables", Short: "Variables and collections (Enterprise plans only)"}
	list := &cobra.Command{
		Use:   "list <ref>",
		Short: "List collections and variables with their values per mode (Tier 2)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fileRead(cmd, globals, args[0], nil, func(ctx context.Context, s *fetch.Session, stream *output.Stream) error {
				which := "local"
				if published {
					which = "published"
				}
				raw, err := s.Client.Get(ctx, "/v1/files/"+s.Key()+"/variables/"+which, nil)
				if err != nil {
					return err
				}
				resp, err := api.Decode[struct {
					Meta struct {
						Variables           map[string]variable           `json:"variables"`
						VariableCollections map[string]variableCollection `json:"variableCollections"`
					} `json:"meta"`
				}](raw, "variables")
				if err != nil {
					return err
				}
				collections := resp.Meta.VariableCollections
				for _, id := range sortedKeys(collections) {
					stream.Item(collectionRecord(collections[id]))
				}
				for _, id := range sortedKeys(resp.Meta.Variables) {
					stream.Item(variableRecord(resp.Meta.Variables[id], collections))
				}
				return nil
			})
		},
	}
	list.Flags().BoolVar(&published, "published", false, "List variables published from this file instead of local ones")
	group.AddCommand(list)
	root.AddCommand(group)
}

func collectionRecord(c variableCollection) map[string]any {
	modes := make([]map[string]string, 0, len(c.Modes))
	defaultMode := ""
	for _, m := range c.Modes {
		modes = append(modes, map[string]string{"id": m.ModeID, "name": m.Name})
		if m.ModeID == c.DefaultModeID {
			defaultMode = m.Name
		}
	}
	return pruned(map[string]any{
		"kind": "collection", "id": c.ID, "name": c.Name, "key": c.Key, "modes": modes,
		"default_mode": defaultMode, "remote": c.Remote, "hidden_from_publishing": c.HiddenFromPublishing,
	})
}

// variableRecord names each value by its mode and renders colours and
// aliases the way node records do.
func variableRecord(v variable, collections map[string]variableCollection) map[string]any {
	c := collections[v.VariableCollectionID]
	modeName := map[string]string{}
	for _, m := range c.Modes {
		modeName[m.ModeID] = m.Name
	}
	values := map[string]any{}
	for mode, raw := range v.ValuesByMode {
		values[cmp.Or(modeName[mode], mode)] = variableValue(raw)
	}
	return pruned(map[string]any{
		"kind": "variable", "id": v.ID, "name": v.Name, "key": v.Key,
		"collection_id": v.VariableCollectionID, "collection": c.Name, "type": strings.ToLower(cmp.Or(v.ResolvedType, v.ResolvedDataType)),
		"description": v.Description, "scopes": v.Scopes, "remote": v.Remote, "code_syntax": v.CodeSyntax, "values": values,
	})
}

func variableValue(raw json.RawMessage) any {
	var alias struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if json.Unmarshal(raw, &alias) == nil && alias.Type == "VARIABLE_ALIAS" {
		return map[string]string{"alias": alias.ID}
	}
	var c figma.Color
	var probe map[string]any
	if json.Unmarshal(raw, &probe) == nil && probe["r"] != nil && json.Unmarshal(raw, &c) == nil {
		return simplify.CSSColor(c)
	}
	var v any
	_ = json.Unmarshal(raw, &v)
	return v
}
