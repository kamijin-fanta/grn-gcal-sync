package main

import (
	"github.com/otoyo/garoon"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type GrnClient struct {
	client *garoon.Client
}

func NewGrnClient(client *garoon.Client) *GrnClient {
	return &GrnClient{
		client: client,
	}
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
	ev, err := g.client.SearchEvents(p)
	if err != nil {
		return nil, err
	}

	// todo pager

	return ev.Events, err
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
