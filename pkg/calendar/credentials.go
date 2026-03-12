package calendar

import "os"

func DefaultOAuthCredentials() OAuthConfig {
	return OAuthConfig{
		ClientID:     os.Getenv("GHOSTWRITER_CLIENT_ID"),
		ClientSecret: os.Getenv("GHOSTWRITER_CLIENT_SECRET"),
	}
}
