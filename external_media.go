package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"html"
	"image"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func publicMediaURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return nil, fmt.Errorf("Нужен публичный HTTPS-адрес")
	}
	return u, nil
}

var externalMediaClient = &http.Client{
	Timeout: 12 * time.Second,
	Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, candidate := range ips {
			ip := candidate.IP
			if ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				dialer := net.Dialer{Timeout: 5 * time.Second}
				if conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port)); err == nil {
					return conn, nil
				}
			}
		}
		return nil, fmt.Errorf("Публичный адрес недоступен")
	}},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("Слишком много перенаправлений")
		}
		_, err := publicMediaURL(req.URL.String())
		return err
	},
}

var mediaMetaTag = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
var mediaMetaAttribute = regexp.MustCompile(`(?i)([a-z:_-]+)\s*=\s*(?:"([^"]*)"|'([^']*)')`)

func articleImageURL(body []byte, origin *url.URL) string {
	for _, tag := range mediaMetaTag.FindAll(body, -1) {
		fields := map[string]string{}
		for _, m := range mediaMetaAttribute.FindAllSubmatch(tag, -1) {
			fields[strings.ToLower(string(m[1]))] = html.UnescapeString(string(m[2]) + string(m[3]))
		}
		if !strings.EqualFold(fields["property"], "og:image") && !strings.EqualFold(fields["name"], "twitter:image") {
			continue
		}
		if fields["content"] == "" {
			continue
		}
		image, err := origin.Parse(fields["content"])
		if err == nil {
			if u, err := publicMediaURL(image.String()); err == nil {
				return u.String()
			}
		}
	}
	return ""
}

func getExternalMedia(ctx context.Context, raw string, max int64) ([]byte, *url.URL, error) {
	u, err := publicMediaURL(raw)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "Malowar News Agent (editorial source preview)")
	res, err := externalMediaClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("Источник вернул HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(body)) > max {
		return nil, nil, fmt.Errorf("Файл источника слишком большой")
	}
	return body, res.Request.URL, nil
}

func (a *app) attachExternalMedia(ctx context.Context, id, raw, kind string) error {
	label := ""
	switch kind {
	case "official":
		label = "Из анонса разработчика · проверь права"
	case "community":
		label = "Из сообщества · проверь автора и права"
	default:
		return fmt.Errorf("Выбери тип источника")
	}
	body, origin, err := getExternalMedia(ctx, raw, 12<<20)
	if err != nil {
		return err
	}
	kindOfImage := http.DetectContentType(body)
	if kindOfImage != "image/jpeg" && kindOfImage != "image/png" {
		imageURL := articleImageURL(body, origin)
		if imageURL == "" {
			return fmt.Errorf("В публикации не найдена открытая иллюстрация. Укажи прямую ссылку JPG или PNG")
		}
		body, _, err = getExternalMedia(ctx, imageURL, 12<<20)
		if err != nil {
			return err
		}
		kindOfImage = http.DetectContentType(body)
	}
	ext := ".jpg"
	if kindOfImage == "image/png" {
		ext = ".png"
	} else if kindOfImage != "image/jpeg" {
		return fmt.Errorf("Картинка должна быть JPG или PNG")
	}
	size, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || size.Width < 430 || size.Height < 190 || size.Width > 12000 || size.Height > 12000 {
		return fmt.Errorf("Неподходящий размер изображения")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	record := a.getItemLocked(id)
	if record == nil || (record.State != "new" && record.State != "rejected") {
		return fmt.Errorf("Сначала открой редактируемый черновик")
	}
	if len(record.Images) >= 7 {
		return fmt.Errorf("Галерея заполнена. Удали одно изображение перед добавлением")
	}
	dir := filepath.Join(a.dataDir, "images", safeID(id))
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	path := filepath.Join(dir, fmt.Sprintf("external-%x%s", sum[:12], ext))
	for _, existing := range record.Images {
		if existing == path {
			return fmt.Errorf("Эта иллюстрация уже добавлена")
		}
	}
	if err = os.WriteFile(path, body, 0600); err != nil {
		return err
	}
	previous := *record
	for len(record.MediaSources) < len(record.Images) {
		record.MediaSources = append(record.MediaSources, "")
	}
	for len(record.MediaLabels) < len(record.Images) {
		record.MediaLabels = append(record.MediaLabels, "")
	}
	record.Images = append(record.Images, path)
	record.MediaSources = append(record.MediaSources, origin.String())
	record.MediaLabels = append(record.MediaLabels, label)
	record.Verified = false
	if len(record.Images) >= 3 {
		record.MediaStatus = "ready"
		record.MediaError = ""
	}
	if err = a.persistLocked(); err != nil {
		*record = previous
		_ = os.Remove(path)
		return err
	}
	return nil
}
