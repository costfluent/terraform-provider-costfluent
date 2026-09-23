package costfluent

import (
	"net/http"
	"strconv"
)

// PageOptions for list endpoints
type PageOptions struct {
	Page  int `url:"page,omitempty"`
	Limit int `url:"limit,omitempty"`
}

// Links for paginated responses (Vantage-style HATEOAS)
type Links struct {
	Self  string `json:"self"`
	First string `json:"first,omitempty"`
	Prev  string `json:"prev,omitempty"`
	Next  string `json:"next,omitempty"`
	Last  string `json:"last,omitempty"`
}

// HasNextPage returns true if there are more pages available
func (l Links) HasNextPage() bool {
	return l.Next != ""
}

// HasPrevPage returns true if there is a previous page
func (l Links) HasPrevPage() bool {
	return l.Prev != ""
}

// ListResponse is the generic paginated response (Vantage-style)
type ListResponse[T any] struct {
	Links Links `json:"links"`
	Data  []T   `json:"data"`
}

func (o *PageOptions) apply(req *http.Request) {
	if o == nil {
		return
	}
	q := req.URL.Query()
	if o.Page > 0 {
		q.Set("page", strconv.Itoa(o.Page))
	}
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	req.URL.RawQuery = q.Encode()
}

// listAll walks every page of a paginated listing.
func listAll[T any](pageSize int, list func(*PageOptions) (*ListResponse[T], error)) ([]T, error) {
	if pageSize <= 0 {
		pageSize = 100
	}

	var all []T
	for page := 1; ; page++ {
		resp, err := list(&PageOptions{Page: page, Limit: pageSize})
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Data...)
		if !resp.Links.HasNextPage() || len(resp.Data) == 0 {
			return all, nil
		}
	}
}
