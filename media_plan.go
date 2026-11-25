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
	"time"
)

// MediaChoice is tied to an official source. It never asserts that an image
// visually depicts a subject just because its text appears in a news title.
type MediaChoice struct {
	URL   string
	Label string
}

var imageLinkRE = regexp.MustCompile(`(?i)https://[^\s<>\[\]"']+`)
var appLinkRE = regexp.MustCompile(`(?i)https?://store\.steampowered\.com/app/([0-9]{2,10})(?:/|\b)`)
var imageExtensionRE = regexp.MustCompile(`(?i)\.(?:png|jpe?g)(?:[?#]|$)`)
var dlcTextRE = regexp.MustCompile(`(?i)(?:\bdlc\b|\bexpansion\b|\bcontent pack\b|\bcountry pack\b|\bflavor pack\b|дополнени[ея]|контент.пак|набор стран)`)

func officialImagesFromNews(contents string) []string {
	raw := html.UnescapeString(contents)
	out := []string{}
	seen := map[string]bool{}
	for _, candidate := range imageLinkRE.FindAllString(raw, -1) {
		candidate = strings.TrimRight(candidate, ").,;:!}")
		if !imageExtensionRE.MatchString(candidate) || !trustedSteamImage(candidate) || seen[candidate] {
			continue
		}
		// Require a clean URL, no userinfo or unexpected protocol.
		u, err := url.Parse(candidate)
		if err != nil || u.User != nil {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
		if len(out) == 12 {
			break
		}
	}
	return out
}

// A news GID is used to fetch the matching full Steam announcement. Reading
// article images from an exact GID avoids promoting unrelated base-game shots.
func officialAnnouncement(ctx context.Context, appID int, gid string) (string, error) {
	if appID <= 0 || gid == "" {
		return "", fmt.Errorf("Нет идентификатора объявления")
	}
	uri := fmt.Sprintf("https://api.steampowered.com/ISteamNews/GetNewsForApp/v2/?appid=%d&count=15&maxlength=0&format=json", appID)
	b, err := get(ctx, uri, nil)
	if err != nil {
		return "", err
	}
	var doc struct {
		AppNews struct {
			News []struct {
				GID      string `json:"gid"`
				Contents string `json:"contents"`
			} `json:"newsitems"`
		} `json:"appnews"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return "", err
	}
	for _, n := range doc.AppNews.News {
		if n.GID == gid {
			return n.Contents, nil
		}
	}
	return "", fmt.Errorf("Полный текст объявления Steam не найден")
}

func exactSteamAppID(it Item, steam []SteamSource) (int, string) {
	if it.Source == "Steam News" {
		parts := strings.Split(it.ID, ":")
		if len(parts) >= 3 {
			id, e := strconv.Atoi(parts[1])
			if e == nil && id > 0 {
				return id, parts[2]
			}
		}
	}
	if strings.HasPrefix(it.Source, "Steam") {
		if m := appLinkRE.FindStringSubmatch(it.URL); len(m) > 1 {
			id, e := strconv.Atoi(m[1])
			if e == nil && id > 0 {
				return id, ""
			}
		}
	}
	// Non-Steam sources can be linked only by an EXACT configured game name.
	// No fuzzy guessing: ambiguous names must be handled by the editor.
	found := 0
	for _, x := range steam {
		if x.AppID > 0 && strings.EqualFold(strings.TrimSpace(x.Name), strings.TrimSpace(it.Game)) {
			if found != 0 && found != x.AppID {
				return 0, ""
			}
			found = x.AppID
		}
	}
	return found, ""
}

func referencedDLCIDs(contents string) []int {
	ids := []int{}
	seen := map[int]bool{}
	for _, m := range appLinkRE.FindAllStringSubmatch(html.UnescapeString(contents), -1) {
		n, e := strconv.Atoi(m[1])
		if e != nil || n <= 0 || seen[n] {
			continue
		}
		ids = append(ids, n)
		seen[n] = true
		if len(ids) >= 5 {
			break
		}
	}
	return ids
}

// Verify that a store link is an actual DLC of the parent game. A link or
// keyword alone is not evidence of a DLC relationship.
func isRelatedDLC(ctx context.Context, id, base int) bool {
	if id == base || id <= 0 {
		return false
	}
	uri := fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%d&l=english&cc=us", id)
	b, e := get(ctx, uri, nil)
	if e != nil {
		return false
	}
	var data map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Type     string `json:"type"`
			FullGame struct {
				AppID int `json:"appid"`
			} `json:"fullgame"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &data) != nil {
		return false
	}
	entry, ok := data[strconv.Itoa(id)]
	return ok && entry.Success && entry.Data.Type == "dlc" && entry.Data.FullGame.AppID == base
}

func makeMediaChoices(urls []string, label string) []MediaChoice {
	out := make([]MediaChoice, 0, len(urls))
	for _, x := range urls {
		if trustedSteamImage(x) {
			out = append(out, MediaChoice{URL: x, Label: label})
		}
	}
	return out
}
func mergeMediaChoices(groups ...[]MediaChoice) []MediaChoice {
	out := []MediaChoice{}
	seen := map[string]bool{}
	for _, group := range groups {
		for _, x := range group {
			if x.URL == "" || seen[x.URL] {
				continue
			}
			out = append(out, x)
			seen[x.URL] = true
			if len(out) >= 24 {
				return out
			}
		}
	}
	return out
}

// mediaPlan uses exact entity identifiers, not generic visual similarity.
// No arbitrary third-party images are fetched. If the announcement supplies
// no official themed images, the fallback is explicitly labeled.
func (a *app) mediaPlan(ctx context.Context, item Item) ([]MediaChoice, string, error) {
	if item.Source == "Workshop" {
		parts := strings.Split(item.ID, ":")
		if len(parts) < 3 {
			return nil, "Модификация: идентификатор Workshop не найден", fmt.Errorf("Нет Workshop ID")
		}
		pics, err := collectWorkshopImages(ctx, parts[1])
		return makeMediaChoices(pics, "Workshop: мод"), "Модификация: материалы из страницы Workshop", err
	}
	a.mu.Lock()
	watch := append([]SteamSource(nil), a.state.Config.Steam...)
	a.mu.Unlock()
	base, gid := exactSteamAppID(item, watch)
	if base <= 0 {
		return nil, "Источник не связан с подтверждённым Steam AppID", fmt.Errorf("Не удалось определить Steam AppID: привяжи источник к игре вручную")
	}
	isRelease := item.Source == "Steam Release Radar" || (strings.Contains(strings.ToLower(item.Title), "released") && item.Source != "Steam News")
	focus := "Базовая игра: официальные изображения Steam"
	if item.Source == "Steam News" {
		focus = "Steam News: изображения из конкретного объявления"
	}
	if isRelease {
		focus = "Релиз игры: обложка и изображения страницы Steam"
	}
	all := [][]MediaChoice{}
	var article string
	var articleErr error
	if gid != "" {
		// Budget fetches independently so an unavailable Steam News API
		// does not prevent the fallback image catalog from being fetched.
		nc, cancel := context.WithTimeout(ctx, 12*time.Second)
		article, articleErr = officialAnnouncement(nc, base, gid)
		cancel()
	}
	fromArticle := makeMediaChoices(officialImagesFromNews(article), "Из объявления Steam (проверь тему)")
	isDLC := dlcTextRE.MatchString(item.Title) || strings.EqualFold(item.Category, "DLC")
	dlcPics := []MediaChoice{}
	if isDLC && len(article) > 0 {
		dlcContext, stop := context.WithTimeout(ctx, 12*time.Second)
		defer stop()
		for _, id := range referencedDLCIDs(article) {
			if dlcContext.Err() != nil {
				break
			}
			nc, cancel := context.WithTimeout(dlcContext, 6*time.Second)
			verified := isRelatedDLC(nc, id, base)
			cancel()
			if !verified {
				continue
			}
			urls, err := collectSteamImages(ctx, id)
			if err == nil {
				dlcPics = makeMediaChoices(urls, "Страница DLC (проверена связь с игрой)")
				break
			}
		}
	}
	if isDLC {
		focus = "DLC: сначала официальный DLC или изображения анонса; общая игра только fallback"
		all = append(all, dlcPics, fromArticle)
	} else if item.Source == "Steam News" {
		all = append(all, fromArticle)
	}
	baseImgs, err := collectSteamImages(ctx, base)
	if err == nil {
		label := "Базовая игра: обложка и геймплей"
		if item.Source != "Steam Release Radar" && !isRelease {
			label = "Fallback: общая страница игры"
		}
		all = append(all, makeMediaChoices(baseImgs, label))
	}
	out := mergeMediaChoices(all...)
	if len(out) > 0 {
		if len(fromArticle) == 0 && len(dlcPics) == 0 && !isRelease {
			focus = "Тематические изображения не подтверждены; показан fallback базовой игры"
		}
		return out, focus, nil
	}
	if articleErr != nil {
		return nil, focus, fmt.Errorf("Не найдены изображения; Steam News: %v; Steam Store: %v", articleErr, err)
	}
	return nil, focus, fmt.Errorf("Официальные изображения не найдены: %v", err)
}
