package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Steam search discovery is best-effort. This is not an exhaustive index of all strategy games.
var searchItemRE = regexp.MustCompile(`(?s)<a\b[^>]*\bdata-ds-appid="([0-9]+)"[^>]*>.*?<span\b[^>]*class="[^"]*title[^"]*"[^>]*>(.*?)</span>`)

func discoverStrategies(ctx context.Context) ([]SteamSource, error) {
	all := []SteamSource{}
	known := map[int]bool{}
	errs := []string{}
	queries := []url.Values{}
	for _, start := range []string{"0", "50", "100", "150", "200", "250"} {
		queries = append(queries, url.Values{"start": {start}, "count": {"50"}, "sort_by": {"Released_DESC"}, "tags": {"9"}, "infinite": {"1"}})
		queries = append(queries, url.Values{"start": {start}, "count": {"50"}, "sort_by": {"Released_ASC"}, "filter": {"comingsoon"}, "tags": {"9"}, "infinite": {"1"}})
	}
	// Third pool: most-wishlisted upcoming strategies, in addition to
	// recently released and chronological upcoming listings.
	for _, start := range []string{"0", "50", "100"} {
		queries = append(queries, url.Values{"start": {start}, "count": {"50"}, "sort_by": {"PopularWishlisted"}, "filter": {"comingsoon"}, "tags": {"9"}, "infinite": {"1"}})
	}
	for _, q := range queries {
		b, err := get(ctx, "https://store.steampowered.com/search/results/?"+q.Encode(), nil)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		var j struct {
			HTML string `json:"results_html"`
		}
		if json.Unmarshal(b, &j) != nil || j.HTML == "" {
			errs = append(errs, "Steam не вернул список игр")
			continue
		}
		for _, m := range searchItemRE.FindAllStringSubmatch(j.HTML, -1) {
			id, e := strconv.Atoi(m[1])
			if e != nil || id <= 0 || known[id] {
				continue
			}
			name := strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(m[2], " ")))
			if name == "" {
				continue
			}
			known[id] = true
			all = append(all, SteamSource{AppID: id, Name: name})
		}
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("поиск Steam не дал результатов: %s", strings.Join(errs, "; "))
	}
	return all, nil
}
