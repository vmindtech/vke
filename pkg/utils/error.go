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

	// App Errors
	FailedToGetAppMsg            = "failed to get app information."
	FailedToGetClusterDetailsMsg = "failed to get cluster details."
	FailedToGetClusterMsg        = "failed to get cluster information."
	FailedToUpdateClusterMsg     = "failed to update cluster information."
	FailedToGetClusterListMsg    = "failed to get cluster list."
	FailedToGetKubeconfigMsg     = "failed to get kubeconfig."
	FailedToDecodeKubeconfigMsg  = "failed to decode kubeconfig."
	FailedToAddNodeMsg           = "failed to add node."
	FailedToGetInstancesMsg      = "failed to get instances."
	FailedToGetNodeGroupsMsg     = "failed to get node groups."
	FailedToGetClusterFlavorMsg  = "failed to get cluster flavor."
	FailedToDeleteNodeGroupMsg   = "failed to delete node group."
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
