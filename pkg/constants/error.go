package constants

// Cluster Error Messages
const (
	// Authentication Errors
	ErrAuthTokenCheckFailed = "Authentication token validation failed"
	ErrAuthTokenInvalid     = "Invalid authentication token provided"
	ErrAuthTokenExpired     = "Authentication token has expired"
	ErrAuthTokenMissing     = "Authentication token is missing"

	// Cluster Creation Errors
	ErrClusterCreateFailed   = "Cluster creation process failed"
	ErrClusterNameInvalid    = "Invalid cluster name provided"
	ErrClusterVersionInvalid = "Invalid Kubernetes version specified"
	ErrClusterProjectInvalid = "Invalid project ID provided"
	ErrClusterSubnetInvalid  = "Invalid subnet configuration"
	ErrClusterKeypairInvalid = "Invalid node keypair name"

	// Cluster Resource Errors
	ErrLoadBalancerCreateFailed          = "Failed to create load balancer for cluster"
	ErrLoadBalancerDeleteFailed          = "Failed to delete load balancer components"
	ErrDNSRecordCreateFailed             = "Failed to create DNS record for cluster"
	ErrDNSRecordDeleteFailed             = "Failed to delete DNS record"
	ErrFloatingIPCreateFailed            = "Failed to create floating IP for cluster"
	ErrFloatingIPDeleteFailed            = "Failed to delete floating IP"
	ErrSecurityGroupCreateFailed         = "Failed to create security groups"
	ErrSecurityGroupDeleteFailed         = "Failed to delete security groups"
	ErrApplicationCredentialCreateFailed = "Failed to create application credential"
	ErrApplicationCredentialDeleteFailed = "Failed to delete application credentials"

	// Node Group Errors
	ErrNodeGroupCreateFailed  = "Failed to create node groups"
	ErrNodeGroupDeleteFailed  = "Failed to delete node groups"
	ErrNodeGroupUpdateFailed  = "Failed to update node groups"
	ErrNodeGroupScalingFailed = "Failed to scale node groups"

	// Network Errors
	ErrNetworkCreateFailed = "Failed to create network components"
	ErrNetworkDeleteFailed = "Failed to delete network components"
	ErrSubnetCreateFailed  = "Failed to create subnet"
	ErrSubnetDeleteFailed  = "Failed to delete subnet"

	// Database Errors
	ErrDatabaseConnectionFailed  = "Database connection failed"
	ErrDatabaseQueryFailed       = "Database query execution failed"
	ErrDatabaseTransactionFailed = "Database transaction failed"
	ErrResourceCreateFailed      = "Failed to create resource record"
	ErrResourceDeleteFailed      = "Failed to delete resource record"
	ErrAuditLogCreateFailed      = "Failed to create audit log entry"

	// Kubernetes Errors
	ErrKubeconfigCreateFailed = "Failed to create kubeconfig"
	ErrKubeconfigUpdateFailed = "Failed to update kubeconfig"
	ErrKubeconfigDeleteFailed = "Failed to delete kubeconfig"
	ErrKubeconfigInvalid      = "Invalid kubeconfig format"

	// Cloudflare Errors
	ErrCloudflareRecordCreateFailed = "Failed to create Cloudflare DNS record"
	ErrCloudflareRecordDeleteFailed = "Failed to delete Cloudflare DNS record"
	ErrCloudflareTokenInvalid       = "Invalid Cloudflare authentication token"

	// OpenStack API Errors
	ErrOpenStackAPIConnectionFailed = "Failed to connect to OpenStack API"
	ErrOpenStackAPIRequestFailed    = "OpenStack API request failed"
	ErrOpenStackAPIResponseInvalid  = "Invalid response from OpenStack API"

	// General System Errors
	ErrSystemResourceExhausted = "System resources exhausted"
	ErrSystemTimeout           = "Operation timed out"
	ErrSystemUnavailable       = "System temporarily unavailable"
	ErrSystemMaintenance       = "System under maintenance"

	// Validation Errors
	ErrValidationFailed   = "Input validation failed"
	ErrValidationRequired = "Required field missing"
	ErrValidationFormat   = "Invalid data format"
	ErrValidationRange    = "Value out of acceptable range"

	// Unknown Errors
	ErrUnknown = "An unknown error occurred"
)

// Error Categories for better organization
const (
	ErrorCategoryAuth       = "authentication"
	ErrorCategoryCluster    = "cluster_operation"
	ErrorCategoryResource   = "resource_management"
	ErrorCategoryNetwork    = "network"
	ErrorCategoryDatabase   = "database"
	ErrorCategoryKubernetes = "kubernetes"
	ErrorCategoryCloudflare = "cloudflare"
	ErrorCategoryOpenStack  = "openstack"
	ErrorCategorySystem     = "system"
	ErrorCategoryValidation = "validation"
	ErrorCategoryUnknown    = "unknown"
)

// GetErrorMessage returns a formatted error message with additional context
func GetErrorMessage(baseMessage, operation, clusterUUID string) string {
	if clusterUUID == "" {
		clusterUUID = "unknown"
	}

	if operation == "" {
		return baseMessage
	}

	return baseMessage + " during " + operation + " for cluster: " + clusterUUID
}

// GetDetailedErrorMessage returns a detailed error message with all context
func GetDetailedErrorMessage(baseMessage, operation, clusterUUID, details string) string {
	msg := GetErrorMessage(baseMessage, operation, clusterUUID)

	if details != "" {
		msg += " - Details: " + details
	}

	return msg
}
