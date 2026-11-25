package deezer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"
)

type Site struct {
	baseURL string
	http    *http.Client

	mu    sync.Mutex
	token string
}

func NewSite() *Site { return NewSiteWithBaseURL("https://www.deezer.com") }

func NewSiteWithBaseURL(base string) *Site {
	jar, _ := cookiejar.New(nil)
	return &Site{baseURL: base, http: &http.Client{Timeout: 10 * time.Second, Jar: jar}}
}

type gwResponse struct {
	Error   json.RawMessage `json:"error"`
	Results json.RawMessage `json:"results"`
}

func (r *gwResponse) errorCodes() map[string]any {
	var codes map[string]any
	json.Unmarshal(r.Error, &codes)
	return codes
}

func (s *Site) gw(ctx context.Context, method, token string, body any) (*gwResponse, error) {
	q := url.Values{"method": {method}, "input": {"3"}, "api_version": {"1.0"}, "api_token": {token}}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/ajax/gw-light.php?"+q.Encode(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deezer site %s: HTTP %d", method, resp.StatusCode)
	}
	var out gwResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("deezer site %s: %w", method, err)
	}
	return &out, nil
}

func (s *Site) apiToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" {
		return s.token, nil
	}
	r, err := s.gw(ctx, "deezer.getUserData", "", struct{}{})
	if err != nil {
		return "", err
	}
	var data struct {
		CheckForm string `json:"checkForm"`
	}
	if json.Unmarshal(r.Results, &data) != nil || data.CheckForm == "" {
		return "", errors.New("deezer site: no api token")
	}
	s.token = data.CheckForm
	return s.token, nil
}

func (s *Site) dropToken(old string) {
	s.mu.Lock()
	if s.token == old {
		s.token = ""
	}
	s.mu.Unlock()
}

func (s *Site) ArtistBio(ctx context.Context, id, lang string) (string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := s.apiToken(ctx)
		if err != nil {
			return "", err
		}
		r, err := s.gw(ctx, "deezer.pageArtist", token, map[string]string{"art_id": id, "lang": lang})
		if err != nil {
			return "", err
		}
		if codes := r.errorCodes(); len(codes) > 0 {
			if _, ok := codes["VALID_TOKEN_REQUIRED"]; ok {
				s.dropToken(token)
				continue
			}
			return "", nil
		}
		var page struct {
			Bio json.RawMessage `json:"BIO"`
		}
		if json.Unmarshal(r.Results, &page) != nil {
			return "", errors.New("deezer site: unexpected artist page")
		}
		var bio struct {
			Bio string `json:"BIO"`
		}
		json.Unmarshal(page.Bio, &bio)
		return bio.Bio, nil
	}
	return "", errors.New("deezer site: token rejected")
}
