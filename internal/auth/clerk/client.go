package clerk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ClerkClient calls the Clerk Backend API. Token *verification* is handled
// separately by internal/auth/jwt against Clerk's public JWKS — this client
// is only for looking up user/organization profile data server-to-server.
type ClerkClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewClerkClient(apiKey, baseURL string) *ClerkClient {
	return &ClerkClient{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// UserInfo represents Clerk user information.
type UserInfo struct {
	ID        string  `json:"id"`
	Email     string  `json:"email"`
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
}

type clerkEmailAddress struct {
	ID           string `json:"id"`
	EmailAddress string `json:"email_address"`
}

type clerkUserResponse struct {
	ID                    string              `json:"id"`
	FirstName             *string             `json:"first_name"`
	LastName              *string             `json:"last_name"`
	PrimaryEmailAddressID string              `json:"primary_email_address_id"`
	EmailAddresses        []clerkEmailAddress `json:"email_addresses"`
}

// GetUser retrieves user information from Clerk's Backend API.
func (c *ClerkClient) GetUser(ctx context.Context, clerkUserID string) (*UserInfo, error) {
	if clerkUserID == "" {
		return nil, fmt.Errorf("clerk user ID is empty")
	}

	var resp clerkUserResponse
	if err := c.get(ctx, fmt.Sprintf("/users/%s", clerkUserID), &resp); err != nil {
		return nil, err
	}

	email := ""
	for _, addr := range resp.EmailAddresses {
		if addr.ID == resp.PrimaryEmailAddressID {
			email = addr.EmailAddress
			break
		}
	}
	if email == "" && len(resp.EmailAddresses) > 0 {
		email = resp.EmailAddresses[0].EmailAddress
	}

	return &UserInfo{
		ID:        resp.ID,
		Email:     email,
		FirstName: resp.FirstName,
		LastName:  resp.LastName,
	}, nil
}

// OrgInfo represents Clerk organization information.
type OrgInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetOrganization retrieves organization information from Clerk.
func (c *ClerkClient) GetOrganization(ctx context.Context, orgID string) (*OrgInfo, error) {
	if orgID == "" {
		return nil, fmt.Errorf("org ID is empty")
	}

	var org OrgInfo
	if err := c.get(ctx, fmt.Sprintf("/organizations/%s", orgID), &org); err != nil {
		return nil, err
	}
	return &org, nil
}

func (c *ClerkClient) get(ctx context.Context, path string, dest interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("failed to build clerk request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("clerk API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("clerk API returned status %d for %s", resp.StatusCode, path)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("failed to decode clerk API response: %w", err)
	}
	return nil
}
