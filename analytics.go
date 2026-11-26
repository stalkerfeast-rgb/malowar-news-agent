package main

import "sort"

// Local editorial metrics; does not claim audience reach or VK engagement.
func (a *app) analytics() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	byStatus := map[string]int{}
	byCategory := map[string]int{}
	byGame := map[string]int{}
	priorityTotal := 0
	for _, it := range a.state.Items {
		byStatus[it.State]++
		byCategory[it.Category]++
		byGame[it.Game]++
		priorityTotal += it.Priority
	}
	type GameCount struct {
		Game  string `json:"game"`
		Count int    `json:"count"`
	}
	games := []GameCount{}
	for game, count := range byGame {
		games = append(games, GameCount{game, count})
	}
	sort.Slice(games, func(i, j int) bool { return games[i].Count > games[j].Count })
	if len(games) > 15 {
		games = games[:15]
	}
	mean := 0
	if len(a.state.Items) > 0 {
		mean = priorityTotal / len(a.state.Items)
	}
	history := append([]AuditEvent(nil), a.state.Audit...)
	if len(history) > 100 {
		history = history[len(history)-100:]
	}
	return map[string]any{"total": len(a.state.Items), "status": byStatus, "category": byCategory, "games": games, "average_priority": mean, "history": history}
}
