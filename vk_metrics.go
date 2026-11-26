package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

type VKMetrics struct {
	Likes     *int      `json:"likes,omitempty"`
	Comments  *int      `json:"comments,omitempty"`
	Reposts   *int      `json:"reposts,omitempty"`
	Views     *int      `json:"views,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

type metricCount struct {
	Count *int `json:"count"`
}
type vkWallEntry struct {
	ID       int64       `json:"id"`
	Likes    metricCount `json:"likes"`
	Comments metricCount `json:"comments"`
	Reposts  metricCount `json:"reposts"`
	Views    metricCount `json:"views"`
}

func decodeVKPosts(raw json.RawMessage) ([]vkWallEntry, error) {
	var entries []vkWallEntry
	if err := json.Unmarshal(raw, &entries); err == nil {
		return entries, nil
	}
	var wrapped struct {
		Items []vkWallEntry `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Items, nil
}

// Manual analytics refresh; read-only VK request, and no artificial zeros for missing counters.
func (a *app) refreshVKMetrics(ctx context.Context) (int, error) {
	a.mu.Lock()
	token := a.token
	group := a.state.Config.GroupID
	type target struct {
		id   string
		post int64
	}
	posts := []target{}
	for _, it := range a.state.Items {
		if it.State == "published" && it.VKPostID > 0 {
			posts = append(posts, target{it.ID, it.VKPostID})
		}
	}
	a.mu.Unlock()
	if token == "" || !validGroup(group) {
		return 0, errors.New("Подключи VK в настройках")
	}
	if len(posts) == 0 {
		return 0, errors.New("Пока нет опубликованных через агента постов")
	}
	sort.Slice(posts, func(i, j int) bool { return posts[i].post > posts[j].post })
	if len(posts) > 30 {
		posts = posts[:30]
	}
	updated := 0
	errorsList := []string{}
	for _, post := range posts {
		response, err := vkCall(ctx, token, "wall.getById", url.Values{"posts": {"-" + group + "_" + fmt.Sprint(post.post)}})
		if err != nil {
			errorsList = append(errorsList, err.Error())
			break
		}
		entries, e := decodeVKPosts(response)
		if e != nil || len(entries) == 0 {
			errorsList = append(errorsList, "VK не вернул опубликованную запись")
			continue
		}
		found := entries[0]
		if found.ID != post.post {
			errorsList = append(errorsList, "VK вернул другой ID")
			continue
		}
		a.mu.Lock()
		if it := a.getItemLocked(post.id); it != nil && it.VKPostID == post.post {
			it.VKStats = &VKMetrics{Likes: found.Likes.Count, Comments: found.Comments.Count, Reposts: found.Reposts.Count, Views: found.Views.Count, CheckedAt: time.Now().UTC()}
			updated++
		}
		a.mu.Unlock()
		time.Sleep(380 * time.Millisecond)
	}
	a.mu.Lock()
	a.auditLocked("metrics", "", fmt.Sprintf("Обновлено %d опубликованных записей", updated))
	_ = a.persistLocked()
	a.mu.Unlock()
	if len(errorsList) > 0 {
		return updated, errors.New(strings.Join(errorsList, "; "))
	}
	return updated, nil
}
