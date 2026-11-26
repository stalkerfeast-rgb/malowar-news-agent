package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var client = &http.Client{Timeout: 14 * time.Second}
var tags = regexp.MustCompile(`(?s)<[^>]*>`)
var bbcode = regexp.MustCompile(`\[/?[a-zA-Z][^\]]{0,100}\]`)
var blank = regexp.MustCompile(`\s+`)

func plain(s string) string {
	s = html.UnescapeString(tags.ReplaceAllString(bbcode.ReplaceAllString(s, " "), " "))
	s = blank.ReplaceAllString(s, " ")
	x := []rune(strings.TrimSpace(s))
	if len(x) > 1600 {
		x = x[:1600]
	}
	return string(x)
}
func get(ctx context.Context, uri string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Malowar-News-Agent/1.0 (+local editorial monitoring)")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, uri)
	}
	return io.ReadAll(io.LimitReader(res.Body, 4<<20))
}
func fetchSteam(ctx context.Context, s SteamSource) ([]Item, error) {
	uri := fmt.Sprintf("https://api.steampowered.com/ISteamNews/GetNewsForApp/v2/?appid=%d&count=15&maxlength=4000&format=json", s.AppID)
	b, err := get(ctx, uri, nil)
	if err != nil {
		return nil, err
	}
	var v struct {
		AppNews struct {
			News []struct {
				GID      string `json:"gid"`
				Title    string `json:"title"`
				URL      string `json:"url"`
				Date     int64  `json:"date"`
				Contents string `json:"contents"`
			} `json:"newsitems"`
		} `json:"appnews"`
	}
	if err = json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	out := []Item{}
	for _, n := range v.AppNews.News {
		if n.GID == "" || n.Title == "" || n.Date <= 0 {
			continue
		}
		it := Item{ID: uid("steam", strconv.Itoa(s.AppID), n.GID), Source: "Steam News", Game: s.Name, Title: plain(n.Title), URL: n.URL, Time: time.Unix(n.Date, 0).UTC(), Excerpt: plain(n.Contents)}
		it.Category = classify(it.Title, it.Source)
		out = append(out, it)
	}
	return out, nil
}
func fetchWorkshop(ctx context.Context, s WorkshopSource) ([]Item, error) {
	vals := url.Values{"itemcount": {"1"}, "publishedfileids[0]": {s.FileID}}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.steampowered.com/ISteamRemoteStorage/GetPublishedFileDetails/v1/", strings.NewReader(vals.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Malowar-News-Agent/1.0")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("Workshop HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var data struct {
		Response struct {
			Details []struct {
				Result          int    `json:"result"`
				Title           string `json:"title"`
				Description     string `json:"file_description"`
				Updated         int64  `json:"time_updated"`
				PublishedFileID string `json:"publishedfileid"`
			} `json:"publishedfiledetails"`
		} `json:"response"`
	}
	if err = json.Unmarshal(b, &data); err != nil {
		return nil, err
	}
	if len(data.Response.Details) == 0 || data.Response.Details[0].Result != 1 {
		return nil, fmt.Errorf("Workshop %s: ответ не содержит открытого файла", s.FileID)
	}
	d := data.Response.Details[0]
	if d.Updated <= 0 {
		return nil, fmt.Errorf("Workshop %s: дата обновления недоступна", s.FileID)
	}
	game := s.Name
	if game == "" {
		game = d.Title
	}
	it := Item{ID: workshopNewsID(s.FileID, d.Updated), Source: "Workshop", Game: game, Title: d.Title + " — обновление мода", URL: workshopItemURL + s.FileID, Time: time.Unix(d.Updated, 0).UTC(), Excerpt: plain(d.Description), Category: "Моды", ParentGame: s.ParentGame}
	return []Item{it}, nil
}
func fetchGitHub(ctx context.Context, repo string) ([]Item, error) {
	b, err := get(ctx, "https://api.github.com/repos/"+repo+"/releases?per_page=10", map[string]string{"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"})
	if err != nil {
		return nil, err
	}
	var v []struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Tag       string `json:"tag_name"`
		Body      string `json:"body"`
		Link      string `json:"html_url"`
		Published string `json:"published_at"`
		Draft     bool   `json:"draft"`
	}
	if err = json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	out := []Item{}
	for _, r := range v {
		if r.Draft || r.ID <= 0 {
			continue
		}
		dt, err := time.Parse(time.RFC3339, r.Published)
		if err != nil {
			continue
		}
		title := r.Name
		if title == "" {
			title = r.Tag
		}
		it := Item{ID: uid("github", repo, strconv.FormatInt(r.ID, 10)), Source: "GitHub Releases", Game: repo, Title: plain(title), URL: r.Link, Time: dt.UTC(), Excerpt: plain(r.Body), Category: "Моды"}
		out = append(out, it)
	}
	return out, nil
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Guid        string `xml:"guid"`
	Date        string `xml:"pubDate"`
	Description string `xml:"description"`
}
type rssDoc struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}
type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}
type atomEntry struct {
	ID        string     `xml:"id"`
	Title     string     `xml:"title"`
	Updated   string     `xml:"updated"`
	Published string     `xml:"published"`
	Summary   string     `xml:"summary"`
	Content   string     `xml:"content"`
	Links     []atomLink `xml:"link"`
}
type atomDoc struct {
	Entries []atomEntry `xml:"entry"`
}

func parseDate(s string) time.Time {
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC3339, time.RFC822Z, time.RFC822, "Mon, 02 Jan 2006 15:04:05 MST"} {
		if d, err := time.Parse(layout, s); err == nil {
			return d.UTC()
		}
	}
	return time.Time{}
}
func fetchRSS(ctx context.Context, s RSSSource) ([]Item, error) {
	b, err := get(ctx, s.URL, nil)
	if err != nil {
		return nil, err
	}
	out := []Item{}
	var r rssDoc
	if xml.Unmarshal(b, &r) == nil && len(r.Channel.Items) > 0 {
		for _, n := range r.Channel.Items {
			d := parseDate(n.Date)
			if d.IsZero() {
				continue
			}
			link := strings.TrimSpace(n.Link)
			if link == "" {
				link = s.URL
			}
			id := link
			if n.Guid != "" {
				id = n.Guid
			}
			it := Item{ID: uid("rss", s.Name, id), Source: "RSS", Game: s.Name, Title: plain(n.Title), URL: link, Time: d, Excerpt: plain(n.Description)}
			it.Category = classify(it.Title, it.Source)
			out = append(out, it)
		}
		return out, nil
	}
	var a atomDoc
	if err := xml.Unmarshal(b, &a); err != nil {
		return nil, err
	}
	for _, n := range a.Entries {
		d := parseDate(n.Published)
		if d.IsZero() {
			d = parseDate(n.Updated)
		}
		if d.IsZero() {
			continue
		}
		link := s.URL
		for _, l := range n.Links {
			if l.Rel == "alternate" || l.Rel == "" {
				if l.Href != "" {
					link = l.Href
					break
				}
			}
		}
		id := n.ID
		if id == "" {
			id = link
		}
		excerpt := n.Summary
		if excerpt == "" {
			excerpt = n.Content
		}
		it := Item{ID: uid("atom", s.Name, id), Source: "RSS/Atom", Game: s.Name, Title: plain(n.Title), URL: link, Time: d, Excerpt: plain(excerpt)}
		it.Category = classify(it.Title, it.Source)
		out = append(out, it)
	}
	return out, nil
}
func (a *app) checkNow() {
	a.mu.Lock()
	if a.checking {
		a.mu.Unlock()
		return
	}
	a.checking = true
	c := a.state.Config
	previousWorkshopRanks := a.state.WorkshopRanks
	lastWorkshopNewsCheck := a.state.WorkshopNewsChecked
	lastWarhammerCheck := a.state.WarhammerChecked
	knownWorkshopNews := map[string]bool{}
	for _, item := range a.state.Items {
		if item.Source == "Workshop" {
			knownWorkshopNews[item.ID] = true
		}
	}
	previousStrategyRanks := a.state.StrategyRanks
	a.mu.Unlock()
	go func() {
		defer func() { a.mu.Lock(); a.checking = false; a.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		ranks := make(chan map[int]workshopRanking, 1)
		workshopNews := make(chan []Item, 1)
		now := time.Now().UTC()
		type franchiseNews struct {
			items   []Item
			checked bool
		}
		warhammer := make(chan franchiseNews, 1)
		go func() {
			if now.Sub(lastWarhammerCheck) < 6*time.Hour {
				warhammer <- franchiseNews{}
				return
			}
			items, checked := collectWarhammerNews(ctx, now)
			warhammer <- franchiseNews{items, checked}
		}()
		scanWorkshopNews := now.Sub(lastWorkshopNewsCheck) >= 6*time.Hour
		subscribedWorkshop := workshopSources(c.Workshop)
		go func() {
			ranking := scanWorkshopRankings(ctx, previousWorkshopRanks, now)
			ranks <- ranking
			if !scanWorkshopNews || len(ranking) == 0 {
				workshopNews <- nil
				return
			}
			subscribed := make(map[string]bool, len(subscribedWorkshop))
			for _, source := range subscribedWorkshop {
				subscribed[source.FileID] = true
			}
			workshopNews <- collectRankedWorkshopNews(ctx, rankedWorkshopUpdates(ranking, knownWorkshopNews, subscribed, now))
		}()
		strategies := make(chan strategyRanking, 1)
		go func() { strategies <- refreshStrategyRanking(ctx, previousStrategyRanks, time.Now().UTC()) }()
		all := []Item{}
		errs := []string{}
		explicit := map[int]bool{}
		for _, s := range append(append([]SteamSource(nil), c.Steam...), priorityReleaseGames...) {
			if explicit[s.AppID] {
				continue
			}
			explicit[s.AppID] = true
			it, e := fetchSteam(ctx, s)
			if e != nil {
				errs = append(errs, "Steam "+s.Name+": "+e.Error())
			} else {
				all = append(all, it...)
			}
		}
		// Hybrids explicitly approved in Interest Score become real monitored
		// Steam News sources too; candidates not approved never do.
		strategyRanks := <-strategies
		steamNews := make([][]Item, len(strategyRanks.Games))
		boundedSteamWork(len(strategyRanks.Games), func(i int) {
			source := strategyRanks.Games[i].SteamSource
			if !explicit[source.AppID] {
				steamNews[i], _ = fetchSteam(ctx, source)
			}
		})
		for i, news := range steamNews {
			explicit[strategyRanks.Games[i].AppID] = true
			all = append(all, news...)
		}
		a.mu.Lock()
		approvedHybridNames := map[int]string{}
		for _, game := range a.state.Radar {
			if game.GenreStatus == "hybrid" {
				approvedHybridNames[game.AppID] = game.Name
			}
		}
		a.mu.Unlock()
		for _, id := range c.HybridApproved {
			if explicit[id] {
				continue
			}
			name := approvedHybridNames[id]
			if name == "" {
				continue
			}
			news, err := fetchSteam(ctx, SteamSource{AppID: id, Name: name})
			if err != nil {
				errs = append(errs, "Steam гибрид "+name+": "+err.Error())
			} else {
				all = append(all, news...)
			}
		}
		for _, s := range subscribedWorkshop {
			it, e := fetchWorkshop(ctx, s)
			if e != nil {
				errs = append(errs, "Workshop "+s.FileID+": "+e.Error())
			} else {
				for i := range it {
					if id := featuredWorkshopID(it[i]); id != "" {
						a.mu.Lock()
						exists := a.getItemLocked(it[i].ID) != nil
						a.mu.Unlock()
						if !exists && !it[i].Time.Before(time.Now().AddDate(0, 0, -14)) {
							enrichWorkshopUpdate(ctx, id, &it[i])
						}
					}
				}
				all = append(all, it...)
			}
		}
		for _, s := range c.GitHub {
			it, e := fetchGitHub(ctx, s)
			if e != nil {
				errs = append(errs, "GitHub "+s+": "+e.Error())
			} else {
				all = append(all, it...)
			}
		}
		for _, s := range c.RSS {
			it, e := fetchRSS(ctx, s)
			if e != nil {
				errs = append(errs, "RSS "+s.Name+": "+e.Error())
			} else {
				all = append(all, it...)
			}
		}
		if c.Discovery {
			newFound, e := discoverStrategies(ctx)
			if e != nil {
				errs = append(errs, "Steam Discovery: "+e.Error())
			}
			a.mu.Lock()
			seen := map[int]bool{}
			for _, x := range c.Steam {
				seen[x.AppID] = true
			}
			for _, x := range a.state.Discovered {
				seen[x.AppID] = true
			}
			for _, x := range newFound {
				if !seen[x.AppID] {
					a.state.Discovered = append(a.state.Discovered, x)
					seen[x.AppID] = true
				}
			}
			// Limit the rolling catalogue; explicitly selected games are unaffected.
			if len(a.state.Discovered) > 1800 {
				a.state.Discovered = a.state.Discovered[len(a.state.Discovered)-1800:]
			}
			// Recheck freshly listed games on every scan so major releases are timely.
			batch := []SteamSource{}
			inBatch := make(map[int]bool, len(explicit))
			for id := range explicit {
				inBatch[id] = true
			}
			for j, source := range newFound {
				if j >= 8 {
					break
				}
				if !inBatch[source.AppID] {
					batch = append(batch, source)
					inBatch[source.AppID] = true
				}
			}
			if len(a.state.Discovered) > 0 {
				n := 0
				for j := 0; j < len(a.state.Discovered) && n < 24; j++ {
					idx := (a.state.ScanCursor + j) % len(a.state.Discovered)
					source := a.state.Discovered[idx]
					if !inBatch[source.AppID] {
						batch = append(batch, source)
						inBatch[source.AppID] = true
						n++
					}
				}
				a.state.ScanCursor = (a.state.ScanCursor + 24) % len(a.state.Discovered)
			}
			a.mu.Unlock()
			for _, x := range batch {
				items, e := fetchSteam(ctx, x)
				if e == nil {
					all = append(all, items...)
				}
			}
		}
		workshopRanks := <-ranks
		all = append(<-workshopNews, all...)
		franchise := <-warhammer
		all = append(all, franchise.items...)
		a.mu.Lock()
		a.state.WorkshopRanks = workshopRanks
		if franchise.checked {
			a.state.WarhammerChecked = now
		}
		if scanWorkshopNews && len(workshopRanks) > 0 && ctx.Err() == nil {
			a.state.WorkshopNewsChecked = now
		}
		a.state.StrategyRanks = strategyRanks
		known := map[string]bool{}
		fingerprints := map[string]bool{}
		for _, it := range a.state.Items {
			known[it.ID] = true
			fingerprints[normalizeNewsFingerprint(it.Game+" "+it.Title)] = true
		}
		cutoff := time.Now().AddDate(0, 0, -14)
		for _, it := range all {
			if it.Time.Before(cutoff) || it.Time.After(time.Now().Add(24*time.Hour)) || it.Title == "" || known[it.ID] {
				continue
			}
			fingerprint := normalizeNewsFingerprint(it.Game + " " + it.Title)
			if it.Source != "Workshop" && fingerprints[fingerprint] {
				continue
			}
			if c.SkipHotfixes && strings.Contains(strings.ToLower(it.Title), "hotfix") {
				continue
			}
			a.enrichBeforeInsertLocked(&it)
			it.State = "new"
			it.Priority = scoreNews(it)
			it.MediaStatus = "pending"
			it.Images = []string{}
			it.Draft = draftText(it)
			it.Comment = defaultComment(it)
			it.Title = plain(it.Title)
			a.state.Items = append(a.state.Items, &it)
			if it.Priority >= a.state.Config.UrgentThreshold {
				a.auditLocked("urgent", it.ID, "Кандидат на срочную новость, оценка не подтверждает важность автоматически")
			}
			known[it.ID] = true
			fingerprints[fingerprint] = true
		}
		if len(a.state.Items) > 1400 {
			sort.Slice(a.state.Items, func(i, j int) bool { return a.state.Items[i].Time.After(a.state.Items[j].Time) })
			a.state.Items = a.state.Items[:1400]
		}
		a.state.LastCheck = time.Now().UTC()
		a.state.LastError = strings.Join(errs, "\n")
		_ = a.persistLocked()
		a.mu.Unlock()
		if c.AutoImages {
			a.prepareMediaBatch()
		}
		if c.AutoRewrite && c.AIProvider != "none" && (c.AIProvider != "ollama" || c.LocalAIEnabled) {
			// Limit automatic AI work and never approve generated text.
			count := 0
			for _, it := range all {
				if count >= 3 {
					break
				}
				a.mu.Lock()
				record := a.getItemLocked(it.ID)
				ready := record != nil && record.State == "new" && record.RewriteStatus == "" && (featuredWorkshopID(*record) == "" || record.Excerpt != "")
				a.mu.Unlock()
				if ready {
					_ = a.rewriteItem(ctx, it.ID)
					count++
				}
			}
		}
	}()
}
