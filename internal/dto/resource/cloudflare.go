package resource

type AddDNSRecordResponse struct {
	Errors  []CFError `json:"errors"`
	Success bool      `json:"success"`
}

type CFError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
