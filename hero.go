package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Users can explicitly replace the last attached image with official artwork
// when a 7-image gallery is already full. Excluded images are not deleted.
func (a *app) promoteOfficialHero(ctx context.Context, id string) error {
	a.mu.Lock()
	it := a.getItemLocked(id)
	if it == nil {
		a.mu.Unlock()
		return errors.New("Новость не найдена")
	}
	if it.State != "new" && it.State != "rejected" {
		a.mu.Unlock()
		return errors.New("Сначала верни пост в черновики")
	}
	snapshot := *it
	snapshot.Images = append([]string(nil), it.Images...)
	snapshot.MediaSources = append([]string(nil), it.MediaSources...)
	snapshot.MediaLabels = append([]string(nil), it.MediaLabels...)
	a.mu.Unlock()
	candidates, focus, err := a.mediaPlan(ctx, snapshot)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return errors.New("Не найдено официальных кандидатов для обложки")
	}
	// Candidate ordering is library hero, official header, then gameplay.
	for i, raw := range candidates {
		if i >= 3 && snapshot.Source == "Steam News" {
			break
		} // favour artwork from the announcement or an exact DLC
		for idx, src := range snapshot.MediaSources {
			if src == raw.URL {
				a.mu.Lock()
				cur := a.getItemLocked(id)
				if cur != nil && (cur.State == "new" || cur.State == "rejected") {
					for len(cur.MediaLabels) < len(cur.Images) {
						cur.MediaLabels = append(cur.MediaLabels, "Не классифицировано")
					}
					cur.MediaLabels[idx] = raw.Label
					cur.MediaFocus = focus
					_ = a.persistLocked()
				}
				a.mu.Unlock()
				return a.moveImage(id, idx, 0)
			}
		}
		dest := filepath.Join(a.dataDir, "images", safeID(id))
		img, e := saveTrustedImage(ctx, raw.URL, dest, 80+i)
		if e != nil {
			continue
		}
		a.mu.Lock()
		cur := a.getItemLocked(id)
		if cur == nil || cur.State != "new" && cur.State != "rejected" {
			a.mu.Unlock()
			os.Remove(img)
			return errors.New("Статус новости изменился")
		}
		for len(cur.MediaSources) < len(cur.Images) {
			cur.MediaSources = append(cur.MediaSources, "")
		}
		for len(cur.MediaLabels) < len(cur.Images) {
			cur.MediaLabels = append(cur.MediaLabels, "Не классифицировано")
		}
		if len(cur.Images) >= 7 {
			cur.Images = cur.Images[:6]
			cur.MediaSources = cur.MediaSources[:6]
			cur.MediaLabels = cur.MediaLabels[:6]
		}
		cur.Images = append([]string{img}, cur.Images...)
		cur.Awards = nil
		cur.AwardsOriginal = ""
		cur.AwardsHero = ""
		cur.MediaSources = append([]string{raw.URL}, cur.MediaSources...)
		cur.MediaLabels = append([]string{raw.Label}, cur.MediaLabels...)
		cur.MediaFocus = focus
		cur.Verified = false
		if len(cur.Images) >= 3 {
			cur.MediaStatus = "ready"
		}
		persistErr := a.persistLocked()
		a.mu.Unlock()
		return persistErr
	}
	return fmt.Errorf("Не удалось получить официальный арт или баннер для %s", snapshot.Game)
}
