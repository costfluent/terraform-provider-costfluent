package costfluent

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Folder represents a folder for organizing resources
type Folder struct {
	Token          string     `json:"token"`
	Name           string     `json:"name"`
	Description    *string    `json:"description,omitempty"`
	ParentToken    *string    `json:"parent_token,omitempty"`
	WorkspaceToken string     `json:"workspace_token"`
	Path           string     `json:"path"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

// FoldersListResponse is the response for listing folders
type FoldersListResponse = ListResponse[Folder]

// ListFolders returns paginated folders
func (c *Client) ListFolders(ctx context.Context, opts *PageOptions) (*FoldersListResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/folders", nil)
	if err != nil {
		return nil, err
	}

	if opts != nil {
		q := req.URL.Query()
		if opts.Page > 0 {
			q.Set("page", fmt.Sprint(opts.Page))
		}
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprint(opts.Limit))
		}
		req.URL.RawQuery = q.Encode()
	}

	var resp FoldersListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListAllFolders fetches all folders across all pages
func (c *Client) ListAllFolders(ctx context.Context, pageSize int) ([]Folder, error) {
	if pageSize <= 0 {
		pageSize = 100
	}

	var all []Folder
	page := 1
	for {
		resp, err := c.ListFolders(ctx, &PageOptions{Page: page, Limit: pageSize})
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Data...)
		if !resp.Links.HasNextPage() || len(resp.Data) == 0 {
			break
		}
		page++
	}
	return all, nil
}

// GetFolder returns a single folder by token
func (c *Client) GetFolder(ctx context.Context, token string) (*Folder, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/folders/"+token, nil)
	if err != nil {
		return nil, err
	}

	var folder Folder
	if err := c.do(req, &folder); err != nil {
		return nil, err
	}
	return &folder, nil
}

// CreateFolderInput for creating a new folder
type CreateFolderInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	ParentToken *string `json:"parent_token,omitempty"`
}

// CreateFolder creates a new folder
func (c *Client) CreateFolder(ctx context.Context, input *CreateFolderInput) (*Folder, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/folders", input)
	if err != nil {
		return nil, err
	}

	var folder Folder
	if err := c.do(req, &folder); err != nil {
		return nil, err
	}
	return &folder, nil
}

// UpdateFolderInput for updating a folder
type UpdateFolderInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	ParentToken *string `json:"parent_token,omitempty"`
}

// UpdateFolder updates an existing folder
func (c *Client) UpdateFolder(ctx context.Context, token string, input *UpdateFolderInput) (*Folder, error) {
	req, err := c.newRequest(ctx, http.MethodPatch, "/api/v1/folders/"+token, input)
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
func (c *Client) DeleteFolder(ctx context.Context, token string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/api/v1/folders/"+token, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}
