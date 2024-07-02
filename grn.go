package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/otoyo/garoon"
	"golang.org/x/oauth2"
)

type GrnClient struct {
	baseUrl string
	hc      *http.Client
}

func NewGrnClient() *GrnClient {
	return &GrnClient{}
}
func (g *GrnClient) EventsByUser(start, end time.Time, userId string) ([]garoon.Event, error) {
	param := SearchEventParams{
		TargetType: "user",
		Target:     userId,
		RangeStart: start,
		RangeEnd:   end,
		Limit:      1000,
	}
	p := param.Build()

	url := fmt.Sprintf("%s/api/v1/schedule/events?%s", g.baseUrl, p.Encode())
	resp, err := g.hc.Get(url)
	if err != nil {
		return nil, err
	}
	buff, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("bad status code in event fetch. code=%d resp=%s", resp.StatusCode, buff)
	}

	var res garoon.EventPager
	err = json.Unmarshal(buff, &res)
	if err != nil {
		return nil, err
	}

	for _, event := range res.Events {
		log.Printf("  %s", event.Subject)
	}

	return res.Events, err
}

type SearchEventParams struct {
	Limit             int
	Offset            int
	Fields            []string
	OrderBy           string // todo
	RangeStart        time.Time
	RangeEnd          time.Time
	Target            string
	TargetType        string // "user" "organization" "facility"
	Keyword           string
	ExcludeFromSearch []string // subject, company, notes, comments
}

func (s *SearchEventParams) Build() url.Values {
	v := url.Values{}
	if s.Limit != 0 {
		v.Set("limit", strconv.Itoa(s.Limit))
	}
	if s.Offset != 0 {
		v.Set("offset", strconv.Itoa(s.Offset))
	}
	if len(s.Fields) != 0 {
		v.Set("fields", strings.Join(s.Fields, ","))
	}
	if s.OrderBy != "" {
		v.Set("orderBy", s.OrderBy)
	}
	if !s.RangeStart.IsZero() {
		v.Set("rangeStart", s.RangeStart.Format(time.RFC3339))
	}
	if !s.RangeEnd.IsZero() {
		v.Set("rangeEnd", s.RangeEnd.Format(time.RFC3339))
	}
	if s.Target != "" {
		v.Set("target", s.Target)
	}
	if s.TargetType != "" {
		v.Set("targetType", s.TargetType)
	}
	if s.Keyword != "" {
		v.Set("keyword", s.Keyword)
	}
	if len(s.ExcludeFromSearch) != 0 {
		v.Set("excludeFromSearch", strings.Join(s.ExcludeFromSearch, ","))
	}
	return v
}

type Oauth2LocalAuthenticator struct {
	config oauth2.Config
}

func (o *Oauth2LocalAuthenticator) Start(
	ctx context.Context,
) (*oauth2.Token, error) {
	u, err := url.Parse(o.config.RedirectURL)
	if err != nil {
		return nil, fmt.Errorf("failed parse redirect url %w", err)
	}
	port := u.Port()

	randBuff := make([]byte, 8)
	_, err = rand.Read(randBuff)
	if err != nil {
		return nil, err
	}
	state := hex.EncodeToString(randBuff)

	m := http.NewServeMux()
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html><html><a href="/start"><button>start</button></a>
		<p>
		注意: 現在の実装は不十分です。コールバック先のURLはhttpsとなっていますが、実際はhttpのサーバが立っています。<br />
		認証が完了した際に際にブラウザのURLバーからURLをhttps->httpに編集してください。`))
	})
	m.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		codeUrl := o.config.AuthCodeURL(state)
		http.Redirect(w, r, codeUrl, http.StatusFound)
	})

	cancel := make(chan struct{}, 1)
	var token *oauth2.Token
	m.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		codeParam := r.URL.Query().Get("code")
		stateParam := r.URL.Query().Get("state")
		if stateParam != state {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("invalid state"))
			return
		}

		_token, err := o.config.Exchange(ctx, codeParam)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("can not exchange token"))
			return
		}

		w.Write([]byte("ok"))
		token = _token

		close(cancel)
	})
	server := &http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: m,
	}
	go func() {
		if err := server.ListenAndServe(); err != nil { // TODO tls
			if errors.Is(err, http.ErrServerClosed) {
				return
			}
			panic(err)
		}
	}()

	fmt.Printf("\n\nAccess in Web Browser: http://localhost:%s/\n", port)

	select {
	case <-cancel:
	case <-ctx.Done():
	}

	server.Shutdown(ctx)
	return token, nil
}
