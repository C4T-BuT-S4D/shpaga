package authutil

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/C4T-BuT-S4D/shpaga/internal/config"
)

func GetCTFTimeOAuthURL(userID string, chatID int64, config *config.Config) (string, error) {
	oauthURL := url.URL{
		Scheme: "https",
		Host:   config.CTFTimeOAuthHost,
		Path:   "/authorize",
	}

	state, err := (&State{
		UserID: userID,
		ChatID: chatID,
	}).Serialize()
	if err != nil {
		return "", fmt.Errorf("marshalling state: %w", err)
	}

	query := url.Values{}
	query.Set("client_id", config.CTFTimeClientID)
	query.Set("redirect_uri", config.CTFTimeRedirectURL)
	query.Set("scope", "profile:read")
	query.Set("response_type", "code")
	query.Set("state", state)

	oauthURL.RawQuery = query.Encode()

	return oauthURL.String(), nil
}

type State struct {
	UserID string `json:"user_id"`
	ChatID int64  `json:"chat_id"`
}

func (s *State) String() string {
	return fmt.Sprintf("State(user=%s, chat=%d)", s.UserID, s.ChatID)
}

// Serialize exists because Go calls MarshalText for structs if it's defined.
func (s *State) Serialize() (string, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("marshalling json: %w", err)
	}
	buf := make([]byte, base64.URLEncoding.EncodedLen(len(raw)))
	base64.URLEncoding.Encode(buf, raw)
	return string(buf), nil
}

func StateFromString(s string) (*State, error) {
	decoded, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decoding base64: %w", err)
	}

	var state State
	if err := json.Unmarshal(decoded, &state); err != nil {
		return nil, fmt.Errorf("unmarshalling json: %w", err)
	}

	return &state, nil
}
