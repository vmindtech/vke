package request

type AddDNSRecordCFRequest struct {
	Content string   `json:"content"`
	Name    string   `json:"name"`
	Proxied bool     `json:"proxied"`
	Type    string   `json:"type"`
	Comment string   `json:"comment"`
	Tags    []string `json:"tags"`
	TTL     int      `json:"ttl"`
}
