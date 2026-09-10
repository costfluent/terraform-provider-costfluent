package costfluent

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
