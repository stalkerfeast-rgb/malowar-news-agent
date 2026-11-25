package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ReleaseGame stores a verified Steam store snapshot, not an AI prediction.
// Unknown or approximate dates never trigger countdowns or release posts.
type ReleaseGame struct {
	AppID                int       `json:"app_id"`
	Name                 string    `json:"name"`
	URL                  string    `json:"url"`
	Description          string    `json:"description"`
	Date                 string    `json:"date,omitempty"` // Exact YYYY-MM-DD, in Moscow calendar date.
	DateText             string    `json:"date_text,omitempty"`
	ExactDate            bool      `json:"exact_date"`
	ComingSoon           bool      `json:"coming_soon"`
	Released             bool      `json:"released"`
	EarlyAccess          bool      `json:"early_access"`
	Russian              string    `json:"russian"` // yes / not_listed / unknown
	RussianVoice         bool      `json:"russian_voice"`
	Editorial            string    `json:"editorial"`
	ObservedAt           time.Time `json:"observed_at"`
	PreviewCreated       bool      `json:"preview_created"`
	ReleaseCreated       bool      `json:"release_created"`
	GenreStatus          string    `json:"genre_status,omitempty"` // strategy / hybrid / review
	GenreEvidence        []string  `json:"genre_evidence,omitempty"`
	DemoAvailable        bool      `json:"demo_available,omitempty"`
	PopularityRank       int       `json:"popularity_rank,omitempty"`
	PopularityPool       int       `json:"popularity_pool,omitempty"`
	PopularityObservedAt time.Time `json:"popularity_observed_at,omitempty"`
	MomentumRank         int       `json:"momentum_rank,omitempty"`
	MomentumKnown        bool      `json:"momentum_known,omitempty"`
	InterestScore        *int      `json:"interest_score,omitempty"`
	ScoreCoverage        int       `json:"score_coverage,omitempty"`
	AwardSuggestions     []string  `json:"award_suggestions,omitempty"`
}

var ruMentionRE = regexp.MustCompile(`(?i)(?:^|[^a-zа-я])(?:russian|русский)(?:$|[^a-zа-я])`)
var ruVoiceRE = regexp.MustCompile(`(?i)(?:russian|русский)\s*<strong>\s*\*`)

func languageStatus(s string) (string, bool) {
	if strings.TrimSpace(s) == "" {
		return "unknown", false
	}
	if ruMentionRE.MatchString(s) {
		return "yes", ruVoiceRE.MatchString(s)
	}
	return "not_listed", false
}
func languageLabel(g ReleaseGame) string {
	switch g.Russian {
	case "yes":
		if g.RussianVoice {
			return "Русский язык: есть (включая озвучку)"
		}
		return "Русский язык: есть"
	case "not_listed":
		return "Русский язык: не заявлен в Steam"
	default:
		return "Русский язык: пока не подтверждён"
	}
}

func dateMSK(t time.Time) string { return t.In(time.FixedZone("MSK", 3*3600)).Format("2006-01-02") }

var steamDateLayouts = []string{"2 Jan, 2006", "Jan 2, 2006", "2 January, 2006", "January 2, 2006", "Jan 02, 2006", "02 Jan, 2006", "2006-01-02", "2 Jan 2006", "January 02, 2006"}

func exactSteamDate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	for _, f := range steamDateLayouts {
		if t, e := time.Parse(f, s); e == nil && t.Year() >= 2000 && t.Year() <= 2100 {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}
func daysUntilMSK(date string, now time.Time) (int, error) {
	a, e := time.Parse("2006-01-02", date)
	if e != nil {
		return 0, e
	}
	b, _ := time.Parse("2006-01-02", dateMSK(now))
	return int(a.Sub(b).Hours() / 24), nil
}

// Parsing is intentionally narrow: only exact dates and explicit coming_soon=false can establish release.
func parseSteamGame(id int, raw []byte, now time.Time) (ReleaseGame, error) {
	var outer map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Type                string `json:"type"`
			Name                string `json:"name"`
			ShortDescription    string `json:"short_description"`
			DetailedDescription string `json:"detailed_description"`
			SupportedLanguages  string `json:"supported_languages"`
			ReleaseDate         struct {
				ComingSoon *bool  `json:"coming_soon"`
				Date       string `json:"date"`
			} `json:"release_date"`
			Genres []struct {
				Description string `json:"description"`
			} `json:"genres"`
			Demos []struct {
				AppID int `json:"appid"`
			} `json:"demos"`
		} `json:"data"`
	}
	if e := json.Unmarshal(raw, &outer); e != nil {
		return ReleaseGame{}, e
	}
	x, ok := outer[strconv.Itoa(id)]
	if !ok || !x.Success || strings.TrimSpace(x.Data.Name) == "" {
		return ReleaseGame{}, errors.New("Steam не вернул достоверную карточку игры")
	}
	if x.Data.ReleaseDate.ComingSoon == nil {
		return ReleaseGame{}, errors.New("Steam не указал статус выхода, публикация блокируется")
	}
	if x.Data.Type != "game" {
		return ReleaseGame{}, errors.New("Карточка Steam не является игрой")
	}
	genreNames := []string{}
	early := false
	for _, gr := range x.Data.Genres {
		genreNames = append(genreNames, gr.Description)
		s := strings.ToLower(gr.Description)
		if strings.Contains(s, "early access") || strings.Contains(s, "ранний доступ") {
			early = true
		}
	}
	status, evidence := strategyEligibility(genreNames, x.Data.ShortDescription+" "+x.Data.DetailedDescription)
	if status == "excluded" {
		return ReleaseGame{}, errors.New("Не подтверждены существенные стратегические элементы")
	}
	lang, voice := languageStatus(x.Data.SupportedLanguages)
	date, exact := exactSteamDate(x.Data.ReleaseDate.Date)
	desc := plain(x.Data.ShortDescription)
	if len([]rune(desc)) < 65 {
		desc = plain(x.Data.DetailedDescription)
	}
	g := ReleaseGame{AppID: id, Name: plain(x.Data.Name), URL: fmt.Sprintf("https://store.steampowered.com/app/%d/", id), Description: desc, Date: date, DateText: plain(x.Data.ReleaseDate.Date), ExactDate: exact, ComingSoon: *x.Data.ReleaseDate.ComingSoon, Russian: lang, RussianVoice: voice, EarlyAccess: early, ObservedAt: now.UTC(), GenreStatus: status, GenreEvidence: evidence, DemoAvailable: len(x.Data.Demos) > 0}
	if exact && !g.ComingSoon {
		days, _ := daysUntilMSK(date, now)
		g.Released = days <= 0
	}
	return g, nil
}

func fetchSteamGame(ctx context.Context, id int, now time.Time) (ReleaseGame, error) {
	uri := fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%d&l=english&cc=us", id)
	body, e := get(ctx, uri, nil)
	if e != nil {
		return ReleaseGame{}, e
	}
	return parseSteamGame(id, body, now)
}

// Search upcoming strategy listings. Discovery is best effort and bounded to limit Steam requests.
func discoverUpcoming(ctx context.Context) ([]SteamSource, error) {
	out := []SteamSource{}
	seen := map[int]bool{}
	for _, start := range []string{"0", "50"} {
		q := url.Values{"start": {start}, "count": {"50"}, "sort_by": {"Released_ASC"}, "filter": {"comingsoon"}, "tags": {"9"}, "category1": {"998"}, "infinite": {"1"}}
		b, e := get(ctx, "https://store.steampowered.com/search/results/?"+q.Encode(), nil)
		if e != nil {
			continue
		}
		var x struct {
			HTML string `json:"results_html"`
		}
		if json.Unmarshal(b, &x) != nil {
			continue
		}
		for _, m := range searchItemRE.FindAllStringSubmatch(x.HTML, -1) {
			id, e := strconv.Atoi(m[1])
			if e != nil || id <= 0 || seen[id] {
				continue
			}
			name := plain(m[2])
			if name == "" {
				continue
			}
			seen[id] = true
			out = append(out, SteamSource{AppID: id, Name: name})
			if len(out) >= 45 {
				return out, nil
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("Steam временно не вернул список ожидаемых стратегий")
	}
	return out, nil
}

// Steam PopularWishlisted is a *relative order* within a public search result,
// NOT the actual wishlist count. Failure produces missing metrics, never zeros.
func discoverPopularUpcoming(ctx context.Context) ([]SteamSource, map[int]int) {
	out := []SteamSource{}
	ranks := map[int]int{}
	q := url.Values{"start": {"0"}, "count": {"100"}, "sort_by": {"PopularWishlisted"}, "filter": {"comingsoon"}, "infinite": {"1"}}
	raw, err := get(ctx, "https://store.steampowered.com/search/results/?"+q.Encode(), nil)
	if err != nil {
		return out, ranks
	}
	var result struct {
		HTML string `json:"results_html"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return out, ranks
	}
	for _, m := range searchItemRE.FindAllStringSubmatch(result.HTML, -1) {
		id, err := strconv.Atoi(m[1])
		if err != nil || id <= 0 || ranks[id] > 0 {
			continue
		}
		name := plain(m[2])
		if name == "" {
			continue
		}
		ranks[id] = len(out) + 1
		out = append(out, SteamSource{AppID: id, Name: name})
		if len(out) >= 100 {
			break
		}
	}
	return out, ranks
}

func mergeCandidateSources(c Config, discovered []SteamSource) []SteamSource {
	out := []SteamSource{}
	seen := map[int]bool{}
	appendUnique := func(x SteamSource) {
		if x.AppID > 0 && !seen[x.AppID] {
			out = append(out, x)
			seen[x.AppID] = true
		}
	}
	for _, x := range c.ReleaseWatch {
		appendUnique(x)
	}
	for _, x := range c.Steam {
		appendUnique(x)
	}
	for _, x := range discovered {
		appendUnique(x)
	}
	if len(out) > 100 {
		out = out[:100]
	}
	return out
}
func releaseEditorial(g ReleaseGame, c Config) string {
	test := Item{Game: g.Name, Source: "Steam"}
	return editorialBucket(test, c)
}

// Retain a wider set for client-side sorting by popularity, rank change and proximity.
func (a *app) radarAllLocked() []ReleaseGame {
	result := []ReleaseGame{}
	now := time.Now()
	for _, game := range a.state.Radar {
		if !a.state.Config.ReleaseIndie && (game.Editorial == "indie" || game.Editorial == "candidate") {
			continue
		}
		if game.ComingSoon || (game.Released && game.ExactDate && func() bool { d, e := daysUntilMSK(game.Date, now); return e == nil && d >= -2 && d <= 0 }()) {
			result = append(result, game)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		si, sj := result[i].InterestScore, result[j].InterestScore
		if si != nil && sj != nil && *si != *sj {
			return *si > *sj
		}
		if (si != nil) != (sj != nil) {
			return si != nil
		}
		return result[i].Name < result[j].Name
	})
	if len(result) > 100 {
		result = result[:100]
	}
	return result
}

// Explicit editor decision. A hybrid is never auto-approved just from tags.
func (a *app) approveHybrid(appID int, approved bool) error {
	if appID <= 0 {
		return errors.New("Некорректный Steam AppID")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var game *ReleaseGame
	for i := range a.state.Radar {
		if a.state.Radar[i].AppID == appID {
			game = &a.state.Radar[i]
			break
		}
	}
	if game == nil || (game.GenreStatus != "review" && game.GenreStatus != "hybrid") {
		return errors.New("Игра не ожидает проверки гибридного жанра")
	}
	approvedIDs := []int{}
	for _, id := range a.state.Config.HybridApproved {
		if id != appID {
			approvedIDs = append(approvedIDs, id)
		}
	}
	if approved {
		approvedIDs = append(approvedIDs, appID)
		game.GenreStatus = "hybrid"
	} else {
		game.GenreStatus = "review"
	}
	a.state.Config.HybridApproved = approvedIDs
	scoreInterest(game, time.Now())
	a.auditLocked("strategy_genre", fmt.Sprint(appID), fmt.Sprintf("Гибридная стратегия: подтверждена=%t", approved))
	return a.persistLocked()
}

func (a *app) radarTopLocked() []ReleaseGame {
	list := append([]ReleaseGame(nil), a.state.Radar...)
	now := time.Now()
	sort.SliceStable(list, func(i, j int) bool {
		da, _ := daysUntilMSK(list[i].Date, now)
		db, _ := daysUntilMSK(list[j].Date, now)
		if list[i].ComingSoon != list[j].ComingSoon {
			return list[i].ComingSoon
		}
		if list[i].ExactDate != list[j].ExactDate {
			return list[i].ExactDate
		}
		if list[i].InterestScore != nil && list[j].InterestScore != nil && *list[i].InterestScore != *list[j].InterestScore {
			return *list[i].InterestScore > *list[j].InterestScore
		}
		if (list[i].InterestScore != nil) != (list[j].InterestScore != nil) {
			return list[i].InterestScore != nil
		}
		if da != db {
			return da < db
		}
		return list[i].Name < list[j].Name
	})
	count := a.state.Config.ReleaseTop
	if count < 1 || count > 30 {
		count = 15
	}
	filtered := []ReleaseGame{}
	for _, g := range list {
		if !a.state.Config.ReleaseIndie && (g.Editorial == "indie" || g.Editorial == "candidate") {
			continue
		}
		// Do not show long-released catalog games as "upcoming".
		if g.ComingSoon || (g.Released && g.ExactDate && func() bool { d, e := daysUntilMSK(g.Date, now); return e == nil && d >= -2 && d <= 0 }()) {
			filtered = append(filtered, g)
		}
		if len(filtered) >= count {
			break
		}
	}
	return filtered
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
func releasePost(g ReleaseGame, preview bool) Item { return releasePostAt(g, preview, time.Now()) }
func releasePostAt(g ReleaseGame, preview bool, now time.Time) Item {
	name := g.Name
	typ := "релиз"
	title := name + " вышла в Steam"
	if preview {
		typ = "анонс"
		title = name + ": готовимся к выходу в Steam"
	}
	if !preview && g.EarlyAccess {
		title = name + ": игра доступна в раннем доступе Steam"
	}
	if g.ExactDate {
		days, err := daysUntilMSK(g.Date, now)
		if err == nil && days == 0 {
			if preview {
				title = name + ": выходит сегодня в Steam"
			} else if g.EarlyAccess {
				title = name + ": ранний доступ начинается сегодня"
			} else {
				title = name + ": вышла сегодня в Steam"
			}
		}
	}
	description := truncateRunes(g.Description, 900)
	if description == "" {
		description = "Описание разработчиков пока не удалось получить. Проверь страницу Steam перед публикацией."
	}
	dateLine := ""
	if g.ExactDate {
		dateLine = "Дата в Steam: " + g.DateText + "."
	}
	lines := []string{title, "", description, "", dateLine, languageLabel(g), "", "#Стратегии #Steam #MalowarGaming #НовостиИгр"}
	draft := ensureGameHashtags(strings.TrimSpace(strings.Join(lines, "\n")), "Релизы", name)
	id := uid("release-radar", strconv.Itoa(g.AppID), g.Date, typ)
	it := Item{ID: id, Source: "Steam Release Radar", Game: name, Title: title, URL: g.URL, Time: time.Now().UTC(), Excerpt: description + "\n" + languageLabel(g) + "\n" + dateLine, Category: "Релизы", State: "new", Draft: draft, Comment: "Источник: " + g.URL, Verified: false, Priority: 75, EventKind: "release"}
	if preview {
		it.Category = "Анонс"
		it.EventKind = "news"
	}
	if g.EarlyAccess && !preview {
		it.Category = "Ранний доступ"
		it.EventKind = "early_access"
	}
	return it
}
func releaseDue(g ReleaseGame, now time.Time, lead int) (preview, day bool) {
	if !g.ExactDate {
		return false, false
	}
	days, e := daysUntilMSK(g.Date, now)
	if e != nil {
		return false, false
	}
	preview = g.ComingSoon && days >= 0 && days <= lead
	// Steam must explicitly stop saying coming_soon, and date must be today (or yesterday for timezone delays).
	day = g.Released && !g.ComingSoon && days >= -1 && days <= 0
	return
}

func (a *app) radarCheckNow() error {
	a.mu.Lock()
	if a.radarBusy {
		a.mu.Unlock()
		return errors.New("Релизный радар уже проверяет Steam")
	}
	a.radarBusy = true
	config := a.state.Config
	a.mu.Unlock()
	go a.scanReleases(config)
	return nil
}
func (a *app) scanReleases(c Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 115*time.Second)
	defer cancel()
	now := time.Now()
	candidates, discoveryErr := discoverUpcoming(ctx)
	popularCandidates, ranks := discoverPopularUpcoming(ctx)
	src := mergeCandidateSources(c, append(popularCandidates, candidates...))
	type result struct {
		game ReleaseGame
		err  error
	}
	ch := make(chan result, len(src))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, source := range src {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				ch <- result{err: ctx.Err()}
				return
			}
			defer func() { <-sem }()
			g, e := fetchSteamGame(ctx, id, now)
			ch <- result{game: g, err: e}
		}(source.AppID)
	}
	wg.Wait()
	close(ch)
	found := []ReleaseGame{}
	for r := range ch {
		if r.err == nil {
			found = append(found, r.game)
		}
	}
	a.mu.Lock()
	old := map[int]ReleaseGame{}
	for _, g := range a.state.Radar {
		old[g.AppID] = g
	}
	createdIDs := []string{}
	for i := range found {
		g := &found[i]
		g.Editorial = releaseEditorial(*g, c)
		if g.GenreStatus == "review" {
			for _, approvedID := range c.HybridApproved {
				if approvedID == g.AppID {
					g.GenreStatus = "hybrid"
					break
				}
			}
		}
		if rank, ok := ranks[g.AppID]; ok {
			applyPopularityObservation(g, old[g.AppID], rank, len(ranks), now)
		} else if previous, ok := old[g.AppID]; ok && previous.PopularityRank > 0 && now.Sub(previous.PopularityObservedAt) < 24*time.Hour && !previous.PopularityObservedAt.IsZero() {
			// Preserve a recent *dated* observation when Steam search is unavailable.
			g.PopularityRank, g.PopularityPool, g.PopularityObservedAt = previous.PopularityRank, previous.PopularityPool, previous.PopularityObservedAt
			g.MomentumRank, g.MomentumKnown = previous.MomentumRank, previous.MomentumKnown
		}
		scoreInterest(g, now)
		if prior, ok := old[g.AppID]; ok {
			if prior.Date == g.Date && g.Date != "" {
				g.PreviewCreated = prior.PreviewCreated
				g.ReleaseCreated = prior.ReleaseCreated
			} else if prior.ExactDate && prior.Date != g.Date {
				// A postponed date invalidates previous approval and cancels a scheduled release.
				for _, previewPost := range []bool{true, false} {
					oldItem := releasePost(prior, previewPost)
					if record := a.getItemLocked(oldItem.ID); record != nil && record.State != "published" && record.State != "publishing" {
						record.Verified = false
						if record.State == "scheduled" || record.State == "approved" {
							record.State = "missed"
							record.ScheduledAt = time.Time{}
						}
						if record.Error == "" {
							record.Error = "Steam изменил дату релиза: старый черновик не действителен, проверь новую дату"
						}
					}
				}
			}
		}
		preview, day := releaseDue(*g, now, c.ReleaseLeadDays)
		if g.GenreStatus == "review" {
			preview, day = false, false
		}
		// Store-readiness does not equal approval to publish. The user always reviews the post.
		if preview && c.ReleasePreviews && !g.PreviewCreated {
			it := releasePostAt(*g, true, now)
			if a.getItemLocked(it.ID) == nil {
				a.state.Items = append(a.state.Items, &it)
				createdIDs = append(createdIDs, it.ID)
			}
			g.PreviewCreated = true
		}
		if day && c.ReleaseDayPosts && !g.ReleaseCreated {
			it := releasePostAt(*g, false, now)
			if a.getItemLocked(it.ID) == nil {
				a.state.Items = append(a.state.Items, &it)
				createdIDs = append(createdIDs, it.ID)
			}
			g.ReleaseCreated = true
		}
	}
	// Retain old data for sources temporarily unavailable; do not mislabel as newly released.
	byID := map[int]ReleaseGame{}
	for _, g := range a.state.Radar {
		byID[g.AppID] = g
	}
	for _, g := range found {
		byID[g.AppID] = g
	}
	a.state.Radar = make([]ReleaseGame, 0, len(byID))
	for _, g := range byID {
		a.state.Radar = append(a.state.Radar, g)
	}
	// Remember failed attempts too, otherwise a blocked Steam endpoint is retried every 30 seconds.
	a.state.RadarChecked = time.Now().UTC()
	if len(found) > 0 {
		a.state.RadarError = ""
	} else if discoveryErr != nil {
		a.state.RadarError = discoveryErr.Error()
	} else {
		a.state.RadarError = "Steam не вернул карточки игр"
	}
	a.radarBusy = false
	_ = a.persistLocked()
	a.mu.Unlock()
	// Media and AI preparation are opt-in via existing settings and never approve or publish.
	if len(createdIDs) > 0 && (c.AutoImages || c.AutoRewrite) {
		go a.prepareReleaseDrafts(createdIDs, c)
	}
}

func (a *app) prepareReleaseDrafts(ids []string, c Config) {
	for i, id := range ids {
		if i >= 3 {
			break
		}
		if c.AutoImages {
			ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
			_ = a.prepareImages(ctx, id)
			cancel()
		}
		if c.AutoRewrite && (c.AIProvider == "openai" || (c.AIProvider == "ollama" && c.LocalAIEnabled)) {
			ctx, cancel := context.WithTimeout(context.Background(), 230*time.Second)
			_ = a.rewriteItem(ctx, id)
			cancel()
		}
	}
}
