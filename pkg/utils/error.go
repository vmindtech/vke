package utils

const (
	NotFoundErrCode     = "not_found"
	ValidationErrCode   = "validation_failed"
	UnexpectedErrCode   = "unexpected_error"
	UnauthorizedErrCode = "unauthorized"
	BodyParserErrCode   = "body_parser_failed"

	NotFoundMsg     = "Not found!"
	UnexpectedMsg   = "An unexpected error has occurred."
	ValidationMsg   = "The given data was invalid."
	UnauthorizedMsg = "Authentication failed."
	BodyParserMsg   = "The given values could not be parsed."
)

type ErrorBag struct {
	Message string `json:"message"`
	Code    string `json:"code"`
	Cause   error  `json:"cause"`
}

func (e ErrorBag) Error() string {
	return e.Cause.Error()
}

func (e ErrorBag) GetCode() string {
	return e.Code
}
