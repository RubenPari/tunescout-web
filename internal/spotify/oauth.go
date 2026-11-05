package spotify

import (
	"golang.org/x/oauth2"
)

// Scopes richiesti all'utente (gli stessi della CLI tunescout).
var Scopes = []string{
	"user-library-read",
	"user-library-modify",
	"user-top-read",
	"playlist-read-private",
	"playlist-modify-private",
	"playlist-modify-public", // richiesto da PUT /me/library
	"user-follow-modify",     // richiesto da PUT /me/library
}

// OAuthConfig restituisce la configurazione del flusso Authorization Code
// lato server (con client secret).
func OAuthConfig(clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:   "https://accounts.spotify.com/authorize",
			TokenURL:  "https://accounts.spotify.com/api/token",
			AuthStyle: oauth2.AuthStyleInHeader,
		},
	}
}
