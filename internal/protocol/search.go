package protocol

type SearchExcerpt struct {
	Before string `json:"before"`
	Match  string `json:"match"`
	After  string `json:"after"`
}

type MessageMatch struct {
	ID        string        `json:"id"`
	Sequence  int64         `json:"sequence"`
	Role      string        `json:"role"`
	Excerpt   SearchExcerpt `json:"excerpt"`
	CreatedAt string        `json:"createdAt"`
}

type MessageSearchPage struct {
	Matches []MessageMatch `json:"matches"`
	Before  int64          `json:"before"`
	HasMore bool           `json:"hasMore"`
}
