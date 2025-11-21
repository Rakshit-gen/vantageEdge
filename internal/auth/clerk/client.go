package clerk

import (
	"context"
	"fmt"
)

type ClerkClient struct {
	apiKey string
}

func NewClerkClient(apiKey string) *ClerkClient {
	return &ClerkClient{apiKey: apiKey}
}

// UserInfo represents Clerk user information
type UserInfo struct {
	ID        string
	Email     string
	FirstName *string
	LastName  *string
}

// GetUser retrieves user information from Clerk
// In a real implementation, this would call Clerk's API
func (c *ClerkClient) GetUser(ctx context.Context, clerkUserID string) (*UserInfo, error) {
	if clerkUserID == "" {
		return nil, fmt.Errorf("clerk user ID is empty")
	}

	// TODO: In production, call Clerk API:
	// GET https://api.clerk.com/v1/users/{user_id}
	// Authorization: Bearer {apiKey}

	return nil, fmt.Errorf("clerk integration not fully implemented")
}

// VerifyToken verifies a JWT token issued by Clerk
func (c *ClerkClient) VerifyToken(ctx context.Context, token string) (bool, error) {
	if token == "" {
		return false, fmt.Errorf("token is empty")
	}

	// TODO: In production, verify the token signature using Clerk's public key
	// This would involve:
	// 1. Getting Clerk's public key
	// 2. Verifying the JWT signature
	// 3. Checking expiration and claims

	return true, nil
}

// OrgInfo represents Clerk organization information
type OrgInfo struct {
	ID   string
	Name string
}

// GetOrganization retrieves organization information from Clerk
func (c *ClerkClient) GetOrganization(ctx context.Context, orgID string) (*OrgInfo, error) {
	if orgID == "" {
		return nil, fmt.Errorf("org ID is empty")
	}

	// TODO: Call Clerk API to get org info
	return nil, fmt.Errorf("clerk integration not fully implemented")
}
