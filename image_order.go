package main

import "errors"

// Changing image order resets editorial approval and keeps source provenance aligned.
func (a *app) moveImage(id string, from, to int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.getItemLocked(id)
	if it == nil {
		return errors.New("Новость не найдена")
	}
	if it.State != "new" && it.State != "rejected" {
		return errors.New("Изменение изображений разрешено только в новых и отклонённых черновиках")
	}
	if from < 0 || from >= len(it.Images) || to < 0 || to >= len(it.Images) {
		return errors.New("Неверный индекс изображения")
	}
	if from == to {
		return nil
	}
	for len(it.MediaSources) < len(it.Images) {
		it.MediaSources = append(it.MediaSources, "")
	}
	for len(it.MediaLabels) < len(it.Images) {
		it.MediaLabels = append(it.MediaLabels, "Не классифицировано")
	}
	img, src, label := it.Images[from], it.MediaSources[from], it.MediaLabels[from]
	it.Images = append(it.Images[:from], it.Images[from+1:]...)
	it.MediaSources = append(it.MediaSources[:from], it.MediaSources[from+1:]...)
	it.MediaLabels = append(it.MediaLabels[:from], it.MediaLabels[from+1:]...)
	it.Images = append(it.Images, "")
	copy(it.Images[to+1:], it.Images[to:])
	it.Images[to] = img
	it.MediaSources = append(it.MediaSources, "")
	copy(it.MediaSources[to+1:], it.MediaSources[to:])
	it.MediaSources[to] = src
	it.MediaLabels = append(it.MediaLabels, "")
	copy(it.MediaLabels[to+1:], it.MediaLabels[to:])
	it.MediaLabels[to] = label
	if from == 0 || to == 0 {
		it.Awards = nil
		it.AwardsOriginal = ""
		it.AwardsHero = ""
	}
	it.Verified = false
	return a.persistLocked()
}
