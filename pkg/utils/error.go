package utils

const (
	NotFoundErrCode     = "404"
	ValidationErrCode   = "509"
	UnexpectedErrCode   = "500"
	UnauthorizedErrCode = "401"
	BodyParserErrCode   = "400"

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
