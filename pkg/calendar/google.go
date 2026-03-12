package calendar

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const googleCalendarBaseURL = "https://www.googleapis.com/calendar/v3"

type GoogleCalendar struct {
	store  *TokenStore
	token  *Token
	config OAuthConfig
	client *http.Client
}

func NewGoogleCalendar(store *TokenStore, config OAuthConfig) (*GoogleCalendar, error) {
	token, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("loading token: %w", err)
	}

	return &GoogleCalendar{
		store:  store,
		token:  token,
		config: config,
		client: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (g *GoogleCalendar) Events(start, end time.Time) ([]Event, error) {
	if err := g.ensureValidToken(); err != nil {
		return nil, fmt.Errorf("token refresh: %w", err)
	}

	params := url.Values{
		"timeMin":      {start.Format(time.RFC3339)},
		"timeMax":      {end.Format(time.RFC3339)},
		"singleEvents": {"true"},
		"orderBy":      {"startTime"},
	}
	reqURL := googleCalendarBaseURL + "/calendars/primary/events?" + params.Encode()

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.token.AccessToken)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET events returned %d: %s", resp.StatusCode, body)
	}

	var result gcalEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding events: %w", err)
	}

	var events []Event
	for _, item := range result.Items {
		if item.Start.DateTime == "" {
			continue
		}

		ev := Event{
			ID:    item.ID,
			Title: item.Summary,
		}

		ev.Start, _ = time.Parse(time.RFC3339, item.Start.DateTime)
		ev.End, _ = time.Parse(time.RFC3339, item.End.DateTime)

		for _, ep := range item.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" {
				ev.MeetingURL = ep.URI
				break
			}
		}

		for _, a := range item.Attendees {
			ev.Attendees = append(ev.Attendees, a.Email)
		}

		events = append(events, ev)
	}

	return events, nil
}

func (g *GoogleCalendar) Close() error {
	return nil
}

func (g *GoogleCalendar) ensureValidToken() error {
	if !g.token.Expired() {
		return nil
	}

	data := url.Values{
		"client_id":     {g.config.ClientID},
		"client_secret": {g.config.ClientSecret},
		"refresh_token": {g.token.RefreshToken},
		"grant_type":    {"refresh_token"},
	}

	resp, err := http.Post(googleTokenURL, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return err
	}
	if tok.Error != "" {
		return fmt.Errorf("%s: %s", tok.Error, tok.ErrorDesc)
	}

	g.token.AccessToken = tok.AccessToken
	g.token.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)

	return g.store.Save(g.token)
}

type gcalEventsResponse struct {
	Items []gcalEvent `json:"items"`
}

type gcalEvent struct {
	ID             string             `json:"id"`
	Summary        string             `json:"summary"`
	Start          gcalDateTime       `json:"start"`
	End            gcalDateTime       `json:"end"`
	Attendees      []gcalAttendee     `json:"attendees"`
	ConferenceData gcalConferenceData `json:"conferenceData"`
}

type gcalDateTime struct {
	DateTime string `json:"dateTime"`
	Date     string `json:"date"`
}

type gcalAttendee struct {
	Email string `json:"email"`
}

type gcalConferenceData struct {
	EntryPoints []gcalEntryPoint `json:"entryPoints"`
}

type gcalEntryPoint struct {
	EntryPointType string `json:"entryPointType"`
	URI            string `json:"uri"`
}
