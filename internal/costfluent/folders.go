package costfluent

import (
	"context"
	"net/http"
	"net/url"
)

// Folder organizes a workspace's cost reports. Folders nest; Children holds the folders directly
// inside this one.
type Folder struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	ParentID    *string  `json:"parentId,omitempty"`
	ReportCount int      `json:"reportCount"`
	Children    []Folder `json:"children,omitempty"`
}

// FolderList is the listing shape: the workspace's folder tree
type FolderList struct {
	Folders []Folder `json:"folders"`
}

// ListFolders returns a workspace's folders
func (c *Client) ListFolders(ctx context.Context, workspaceID string) (*FolderList, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodGet, "/v1/folders", workspaceID, nil)
	if err != nil {
		return nil, err
	}

	var list FolderList
	if err := c.do(req, &list); err != nil {
		return nil, err
	}
	return &list, nil
}

// GetFolder returns a single folder by ID
func (c *Client) GetFolder(ctx context.Context, workspaceID, id string) (*Folder, error) {
	req, err := c.newWorkspaceRequest(ctx, http.MethodGet, "/v1/folders/"+url.PathEscape(id), workspaceID, nil)
	if err != nil {
		return nil, err
	}

	var folder Folder
	if err := c.do(req, &folder); err != nil {
		return nil, err
	}
	return &folder, nil
}

// CreateFolderInput for creating a new folder. WorkspaceID falls back to the client's default
// workspace.
type CreateFolderInput struct {
	WorkspaceID string  `json:"workspaceId"`
	Title       string  `json:"title"`
	ParentID    *string `json:"parentId,omitempty"`
}

// CreateFolder creates a new folder and returns it as stored
func (c *Client) CreateFolder(ctx context.Context, input *CreateFolderInput) (*Folder, error) {
	body := *input
	var err error
	if body.WorkspaceID, err = c.requireWorkspace(body.WorkspaceID); err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/folders", &body)
	if err != nil {
		return nil, err
	}

	var created createdRef
	if err := c.do(req, &created); err != nil {
		return nil, err
	}
	return c.GetFolder(ctx, body.WorkspaceID, created.ID)
}

// UpdateFolderInput for renaming or moving a folder; unset fields are left as they are.
// WorkspaceID falls back to the client's default workspace.
type UpdateFolderInput struct {
	WorkspaceID string  `json:"workspaceId"`
	Title       *string `json:"title,omitempty"`
	ParentID    *string `json:"parentId,omitempty"`
}

// UpdateFolder updates an existing folder
func (c *Client) UpdateFolder(ctx context.Context, id string, input *UpdateFolderInput) (*Folder, error) {
	body := *input
	var err error
	if body.WorkspaceID, err = c.requireWorkspace(body.WorkspaceID); err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, http.MethodPut, "/v1/folders/"+url.PathEscape(id), &body)
	if err != nil {
		return nil, err
	}

	var folder Folder
	if err := c.do(req, &folder); err != nil {
		return nil, err
	}
	return &folder, nil
}

// DeleteFolder removes a folder
func (c *Client) DeleteFolder(ctx context.Context, workspaceID, id string) error {
	req, err := c.newWorkspaceRequest(ctx, http.MethodDelete, "/v1/folders/"+url.PathEscape(id), workspaceID, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}
