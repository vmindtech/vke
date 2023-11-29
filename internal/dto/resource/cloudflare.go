package resource

type AddDNSRecordResponse struct {
	Errors  []CFError `json:"errors"`
	Success bool      `json:"success"`
	Result  Result    `json:"result"`
}

type Result struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CFError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
