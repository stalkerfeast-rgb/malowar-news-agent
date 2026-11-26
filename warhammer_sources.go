package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"
)

const warhammerCommunity = "https://www.warhammer-community.com"
const warhammerBookshop = "https://www.libhammer.ru"

var warhammerArticleLinks = regexp.MustCompile(`(?i)href="(/en-gb/articles/[a-z0-9]+/[a-z0-9-]+/)"`)
var warhammerArticlePath = regexp.MustCompile(`(?i)^/en-gb/articles/[a-z0-9]+/[a-z0-9-]+/$`)
var warhammerArticleJSON = regexp.MustCompile(`(?is)<script\b[^>]*type="application/ld\+json"[^>]*>(.*?)</script>`)
var warhammerBookLinks = regexp.MustCompile(`(?is)<div class="item-title"[^>]*>\s*<a href="(/catalog/warhammer_books/(?:[a-z_0-9]+/)*[0-9]+/)">\s*<span>(.*?)</span>`)
var russianEdition = regexp.MustCompile(`(?i:язык\s*:?\s*русский|на русском языке)|[Пп]еревод\s+[А-ЯЁ][а-яё]+\s+[А-ЯЁ][а-яё]+`)
var bookDescription = regexp.MustCompile(`(?is)<meta property="og:description" content="([^"]+)"`)
var bookCover = regexp.MustCompile(`(?is)<meta property="og:image" content="(https://www\.libhammer\.ru/upload/[^"]+)"`)

func collectWarhammerNews(ctx context.Context, now time.Time) ([]Item, bool) {
	community, communityErr := get(ctx, warhammerCommunity+"/en-gb/", nil)
	books, booksErr := get(ctx, warhammerBookshop+"/", nil)
	sitemap, _ := get(ctx, warhammerCommunity+"/sitemap.xml", nil)
	type source struct {
		path string
		name string
		book bool
	}
	var sources []source
	seen := map[string]bool{}
	var paths []string
	for _, link := range warhammerArticleLinks.FindAllSubmatch(community, -1) {
		paths = append(paths, string(link[1]))
	}
	paths = append(paths, recentWarhammerPaths(sitemap, now)...)
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		sources = append(sources, source{path: path})
		if len(sources) == 24 {
			break
		}
	}
	for _, entry := range russianBookListings(books) {
		sources = append(sources, source{path: entry[0], name: entry[1], book: true})
	}
	results := make([]Item, len(sources))
	boundedSteamWork(len(sources), func(i int) {
		entry := sources[i]
		if entry.book {
			url := warhammerBookshop + entry.path
			if page, err := get(ctx, url, nil); err == nil {
				results[i] = parseWarhammerBook(page, url, entry.name, now)
			}
		} else {
			url := warhammerCommunity + entry.path
			if page, err := get(ctx, url, nil); err == nil {
				results[i] = parseWarhammerArticle(page, url, now)
			}
		}
	})
	var items []Item
	for _, item := range results {
		if item.ID != "" {
			items = append(items, item)
		}
	}
	return items, communityErr == nil || booksErr == nil
}

func recentWarhammerPaths(data []byte, now time.Time) []string {
	var index struct {
		URLs []struct {
			URL          string    `xml:"loc"`
			LastModified time.Time `xml:"lastmod"`
		} `xml:"url"`
	}
	if xml.Unmarshal(data, &index) != nil {
		return nil
	}
	sort.SliceStable(index.URLs, func(i, j int) bool { return index.URLs[i].LastModified.After(index.URLs[j].LastModified) })
	var paths []string
	for _, entry := range index.URLs {
		if entry.LastModified.Before(now.AddDate(0, 0, -14)) {
			break
		}
		path, ok := strings.CutPrefix(entry.URL, warhammerCommunity)
		if ok && warhammerArticlePath.MatchString(path) && warhammerTopic(path) != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func warhammerTopic(text string) string {
	text = strings.ToLower(text)
	if strings.Contains(text, "black-library") || strings.Contains(text, "black library") {
		return ""
	}
	for _, marker := range []string{"dawn of war", "dawn-of-war", "total war", "total-war", "video game", "video-game", "space marine 2", "space-marine-2", "darktide", "rogue trader", "rogue-trader", "tacticus", "mechanicus"} {
		if strings.Contains(text, marker) {
			return "Игры"
		}
	}
	for _, marker := range []string{"miniature", "reinforcement", "pre-order", "preorder", "battleforce", "combat patrol", "combat-patrol", "army set", "army-set", "sunday preview", "sunday-preview", "next week", "next-week", "boxed set", "new models", "model reveal"} {
		if strings.Contains(text, marker) {
			return "Наборы"
		}
	}
	return ""
}

func parseWarhammerArticle(body []byte, url string, now time.Time) Item {
	for _, match := range warhammerArticleJSON.FindAllSubmatch(body, -1) {
		var data struct {
			Type      string               `json:"@type"`
			Title     string               `json:"headline"`
			Excerpt   string               `json:"description"`
			Published string               `json:"datePublished"`
			Image     struct{ URL string } `json:"image"`
		}
		if json.Unmarshal(match[1], &data) != nil || data.Type != "Article" || plain(data.Title) == "" {
			continue
		}
		date, err := time.Parse(time.DateOnly, data.Published)
		if err != nil || date.Before(now.AddDate(0, 0, -14)) || date.After(now.Add(24*time.Hour)) {
			return Item{}
		}
		topic := warhammerTopic(url + " " + data.Title + " " + data.Excerpt)
		title := strings.ToLower(data.Title)
		if topic == "" || strings.Contains(title, "black library") || strings.Contains(title, "world championships") || strings.Contains(title, "stats at a glance") {
			return Item{}
		}
		image := ""
		if strings.HasPrefix(data.Image.URL, "https://assets.warhammer-community.com/") {
			image = data.Image.URL
		}
		return Item{ID: uid("warhammer-community", safeID(url)), Game: "Warhammer", Title: plain(data.Title), Source: "Warhammer Community", URL: url, Excerpt: plain(data.Excerpt), Time: date, Category: topic, SourceImage: image}
	}
	return Item{}
}

func russianBookListings(body []byte) [][2]string {
	s := string(body)
	start := strings.Index(s, "NEW_wrapp")
	if start < 0 {
		return nil
	}
	block := s[start:]
	if end := strings.Index(block, "STOCK_wrapp"); end >= 0 {
		block = block[:end]
	}
	var listings [][2]string
	seen := map[string]bool{}
	for _, match := range warhammerBookLinks.FindAllStringSubmatch(block, -1) {
		path, title := match[1], plain(match[2])
		if title == "" || seen[path] || strings.Contains(strings.ToLower(title), "уценка") {
			continue
		}
		seen[path] = true
		listings = append(listings, [2]string{path, title})
		if len(listings) >= 10 {
			break
		}
	}
	return listings
}

func parseWarhammerBook(body []byte, url, title string, now time.Time) Item {
	// The storefront's Russian UI is not evidence of the edition language.
	if !russianEdition.MatchString(blank.ReplaceAllString(html.UnescapeString(tags.ReplaceAllString(string(body), " ")), " ")) {
		return Item{}
	}
	excerpt := "Русскоязычное издание найдено в разделе новинок магазина. Дата выхода у издателя не подтверждена."
	if match := bookDescription.FindSubmatch(body); len(match) == 2 {
		excerpt += " " + plain(string(match[1]))
	}
	image := ""
	if match := bookCover.FindSubmatch(body); len(match) == 2 {
		image = html.UnescapeString(string(match[1]))
	}
	return Item{ID: uid("warhammer-ru-book", safeID(url)), Game: "Warhammer", Title: title + " — новинка в каталоге", Source: "Libhammer", URL: url, Excerpt: excerpt, Time: now, Category: "Книги", SourceImage: image}
}
