// Package figma holds the Figma REST API response shapes agent-figma reads:
// the subset it uses, not the whole spec.
package figma

import "encoding/json"

// FileResponse is GET /v1/files/:key.
type FileResponse struct {
	Name          string                  `json:"name"`
	Role          string                  `json:"role"`
	LastModified  string                  `json:"lastModified"`
	EditorType    string                  `json:"editorType"`
	ThumbnailURL  string                  `json:"thumbnailUrl"`
	Version       string                  `json:"version"`
	Document      json.RawMessage         `json:"document"`
	Components    map[string]Component    `json:"components"`
	ComponentSets map[string]ComponentSet `json:"componentSets"`
	Styles        map[string]Style        `json:"styles"`
	MainFileKey   string                  `json:"mainFileKey"`
}

// NodesResponse is GET /v1/files/:key/nodes. A null entry is an id Figma
// could not find.
type NodesResponse struct {
	Name         string                `json:"name"`
	Role         string                `json:"role"`
	LastModified string                `json:"lastModified"`
	EditorType   string                `json:"editorType"`
	ThumbnailURL string                `json:"thumbnailUrl"`
	Version      string                `json:"version"`
	Nodes        map[string]*NodeEntry `json:"nodes"`
}

type NodeEntry struct {
	Document      json.RawMessage         `json:"document"`
	Components    map[string]Component    `json:"components"`
	ComponentSets map[string]ComponentSet `json:"componentSets"`
	Styles        map[string]Style        `json:"styles"`
}

// ImagesResponse is GET /v1/images/:key. A null URL is a node that did not
// render (missing, invisible, or empty).
type ImagesResponse struct {
	Err    *string            `json:"err"`
	Images map[string]*string `json:"images"`
}

// FillsResponse is GET /v1/files/:key/images, which, unlike /v1/images, nests
// its map under meta.
type FillsResponse struct {
	Meta struct {
		Images map[string]string `json:"images"`
	} `json:"meta"`
}

// MetaResponse is GET /v1/files/:key/meta. Version is optional there.
type MetaResponse struct {
	File FileMeta `json:"file"`
}

type FileMeta struct {
	Name          string `json:"name"`
	FolderName    string `json:"folder_name"`
	LastTouchedAt string `json:"last_touched_at"`
	EditorType    string `json:"editorType"`
	Version       string `json:"version"`
	Role          string `json:"role"`
	LinkAccess    string `json:"link_access"`
	ThumbnailURL  string `json:"thumbnail_url"`
	URL           string `json:"url"`
	Creator       *User  `json:"creator,omitempty"`
	LastTouchedBy *User  `json:"last_touched_by,omitempty"`
}

type User struct {
	ID     string `json:"id"`
	Handle string `json:"handle"`
	Email  string `json:"email,omitempty"`
}

// Component is an entry of a file response's components map (node id → it).
type Component struct {
	Key            string `json:"key"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	ComponentSetID string `json:"componentSetId,omitempty"`
	Remote         bool   `json:"remote"`
}

type ComponentSet struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Remote      bool   `json:"remote"`
}

type Style struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	StyleType   string `json:"styleType"`
	Description string `json:"description,omitempty"`
	Remote      bool   `json:"remote"`
}

// PublishedComponent is a library entry from the components, component sets,
// and styles endpoints (Tier 3).
type PublishedComponent struct {
	Key             string           `json:"key"`
	FileKey         string           `json:"file_key"`
	NodeID          string           `json:"node_id"`
	ThumbnailURL    string           `json:"thumbnail_url"`
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	CreatedAt       string           `json:"created_at"`
	UpdatedAt       string           `json:"updated_at"`
	StyleType       string           `json:"style_type,omitempty"`
	ContainingFrame *ContainingFrame `json:"containing_frame,omitempty"`
}

type ContainingFrame struct {
	NodeID                 string `json:"nodeId"`
	Name                   string `json:"name"`
	PageID                 string `json:"pageId"`
	PageName               string `json:"pageName"`
	ContainingComponentSet *struct {
		NodeID string `json:"nodeId"`
		Name   string `json:"name"`
	} `json:"containingComponentSet,omitempty"`
}
