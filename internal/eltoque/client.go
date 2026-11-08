package eltoque

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/timeutil"
)

const defaultBaseURL = "https://tasas.eltoque.com/v1/trmi"

var havanaLocation = timeutil.Havana

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	attempts   int
	backoff    time.Duration
	now        func() time.Time
}

type Rates struct {
	SourceDate string
	SourceTS   int64
	Tasas      map[string]float64
}

func NewClient(baseURL, apiKey string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		attempts: 3,
		backoff:  2 * time.Second,
		now:      time.Now,
	}
}

func (c *Client) Fetch(ctx context.Context) (Rates, error) {
	if c.apiKey == "" {
		return Rates{}, errors.New("eltoque: api key is empty")
	}

	now := c.now().In(havanaLocation)
	endpoint, err := c.endpoint(now.Add(-2*time.Hour), now)
	if err != nil {
		return Rates{}, err
	}

	var lastErr error
	for attempt := 1; attempt <= c.attempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return Rates{}, ctx.Err()
			case <-time.After(time.Duration(attempt-1) * c.backoff):
			}
		}

		rates, err := c.fetchOnce(ctx, endpoint)
		if err == nil {
			return rates, nil
		}
		lastErr = err
	}

	return Rates{}, fmt.Errorf("eltoque: fetch failed after %d attempts: %w", c.attempts, lastErr)
}

func (c *Client) endpoint(from, to time.Time) (string, error) {
	parsed, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("eltoque: invalid base url: %w", err)
	}
	query := parsed.Query()
	query.Set("date_from", from.Format("2006-01-02 15:04:05"))
	query.Set("date_to", to.Format("2006-01-02 15:04:05"))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (c *Client) fetchOnce(ctx context.Context, endpoint string) (Rates, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Rates{}, fmt.Errorf("eltoque: build request: %w", err)
	}
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Authorization", "Bearer "+c.apiKey)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return Rates{}, fmt.Errorf("eltoque: request: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Rates{}, fmt.Errorf("eltoque: read response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		detail := strings.TrimSpace(string(body))
		if len(detail) > 512 {
			detail = detail[:512]
		}
		return Rates{}, fmt.Errorf("eltoque: unexpected status %d: %s", response.StatusCode, detail)
	}

	var raw rawResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return Rates{}, fmt.Errorf("eltoque: decode response: %w", err)
	}

	return c.parse(raw, c.now().In(havanaLocation))
}

func (c *Client) parse(raw rawResponse, fallback time.Time) (Rates, error) {
	if raw.Date == "" {
		return Rates{}, errors.New("eltoque: response without date")
	}

	sourceDate := fmt.Sprintf("%s %02d:%02d:%02d", raw.Date, raw.Hour, raw.Minutes, raw.Seconds)
	sourceTS := fallback.Unix()
	if parsed, err := time.ParseInLocation("2006-01-02 15:04:05", sourceDate, havanaLocation); err == nil {
		sourceTS = parsed.Unix()
	}

	tasas := make(map[string]float64, len(raw.Tasas))
	for code, value := range raw.Tasas {
		if value == nil {
			continue
		}
		code = strings.TrimSpace(strings.ToUpper(code))
		if code == "" {
			continue
		}
		tasas[code] = *value
	}
	if len(tasas) == 0 {
		return Rates{}, errors.New("eltoque: response without rates")
	}

	return Rates{SourceDate: sourceDate, SourceTS: sourceTS, Tasas: tasas}, nil
}

type rawResponse struct {
	Date    string              `json:"date"`
	Hour    int                 `json:"hour"`
	Minutes int                 `json:"minutes"`
	Seconds int                 `json:"seconds"`
	Tasas   map[string]*float64 `json:"tasas"`
}

func SortedCodes(tasas map[string]float64) []string {
	codes := make([]string, 0, len(tasas))
	for code := range tasas {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}
