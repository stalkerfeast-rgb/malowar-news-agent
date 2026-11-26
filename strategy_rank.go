package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Editorial pool of released strategies. Games in the Paradox rubric have their own feed.
var rankedStrategies = []SteamSource{
	{1934680, "Age of Mythology: Retold"}, {1466860, "Age of Empires IV"}, {813780, "Age of Empires II: Definitive Edition"},
	{1017900, "Age of Empires: Definitive Edition"}, {933110, "Age of Empires III: Definitive Edition"}, {221380, "Age of Empires II (2013)"},
	{1142710, "Total War: Warhammer III"}, {594570, "Total War: Warhammer II"}, {364360, "Total War: Warhammer"},
	{779340, "Total War: Three Kingdoms"}, {214950, "Total War: Rome II"}, {34330, "Total War: Shogun 2"},
	{325610, "Total War: Attila"}, {10500, "Empire: Total War"}, {34030, "Napoleon: Total War"}, {4700, "Medieval II: Total War"},
	{1937780, "Total War: Pharaoh"}, {289070, "Sid Meier's Civilization VI"}, {8930, "Sid Meier's Civilization V"},
	{1295660, "Sid Meier's Civilization VII"}, {65980, "Sid Meier's Civilization: Beyond Earth"}, {8800, "Sid Meier's Civilization IV: Beyond the Sword"},
	{1669000, "Age of Wonders 4"}, {718850, "Age of Wonders: Planetfall"}, {226840, "Age of Wonders III"},
	{1124300, "Humankind"}, {597180, "Old World"}, {392110, "Endless Space 2"}, {289130, "Endless Legend"},
	{1531540, "Distant Worlds 2"}, {1575940, "Sins of a Solar Empire II"}, {204880, "Sins of a Solar Empire: Rebellion"},
	{1604270, "Broken Arrow"}, {1611600, "WARNO"}, {251060, "Wargame: Red Dragon"}, {919640, "Steel Division 2"},
	{400750, "Call to Arms - Gates of Hell: Ostfront"}, {302670, "Call to Arms"}, {244450, "Men of War: Assault Squad 2"},
	{1128860, "Men of War II"}, {1677280, "Company of Heroes 3"}, {231430, "Company of Heroes 2"}, {228200, "Company of Heroes"},
	{1295500, "Warhammer 40,000: Battlesector"}, {489630, "Warhammer 40,000: Gladius - Relics of War"},
	{4570, "Warhammer 40,000: Dawn of War"}, {15620, "Warhammer 40,000: Dawn of War II"},
	{466560, "Northgard"}, {1605220, "Dune: Spice Wars"}, {1363080, "Manor Lords"}, {294100, "RimWorld"},
	{323190, "Frostpunk"}, {1601580, "Frostpunk 2"}, {690830, "Foundation"}, {1044720, "Farthest Frontier"},
	{1062090, "Timberborn"}, {1336490, "Against the Storm"}, {2140020, "Stronghold: Definitive Edition"},
	{40970, "Stronghold Crusader HD"}, {644930, "They Are Billions"},
	{1272320, "Diplomacy is Not an Option"}, {1029780, "Going Medieval"}, {867210, "Songs of Conquest"},
	{2109370, "The Great War: Western Front"}, {281610, "Homeworld: Deserts of Kharak"}, {244160, "Homeworld Remastered Collection"},
	{40100, "Supreme Commander 2"}, {9420, "Supreme Commander: Forged Alliance"}, {386070, "Planetary Annihilation: TITANS"},
	{1264280, "Slipways"}, {1202130, "Starship Troopers: Terran Command"}, {1486920, "Tempest Rising"},
	{2012510, "Stormgate"}, {268500, "XCOM 2"}, {200510, "XCOM: Enemy Unknown"}, {365360, "Battle Brothers"},
	{1084160, "Jagged Alliance 3"}, {1213210, "Command & Conquer Remastered Collection"}, {17480, "Command & Conquer Red Alert 3"},
	{24790, "Command & Conquer 3 Tiberium Wars"}, {916440, "Anno 1800"}, {784150, "Workers & Resources: Soviet Republic"},
	{1066780, "Transport Fever 2"}, {3024040, "Stronghold Crusader: Definitive Edition"},
}

type strategySample struct {
	At      time.Time `json:"at"`
	Players int       `json:"players"`
}

type strategyRank struct {
	SteamSource
	Average int `json:"average_players"`
	Samples int `json:"samples"`
}

type strategyRanking struct {
	Checked time.Time                `json:"checked"`
	History map[int][]strategySample `json:"history,omitempty"`
	Games   []strategyRank           `json:"games,omitempty"`
}

func boundedSteamWork(n int, work func(int)) {
	jobs := make(chan int, n)
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for i := 0; i < min(n, 6); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				work(job)
			}
		}()
	}
	workers.Wait()
}

func refreshStrategyRanking(ctx context.Context, old strategyRanking, now time.Time) strategyRanking {
	if now.Sub(old.Checked) < 6*time.Hour {
		return old
	}
	counts := make([]*int, len(rankedStrategies))
	boundedSteamWork(len(rankedStrategies), func(i int) {
		source := rankedStrategies[i]
		raw, err := get(ctx, fmt.Sprintf("https://api.steampowered.com/ISteamUserStats/GetNumberOfCurrentPlayers/v1/?appid=%d", source.AppID), nil)
		var response struct {
			Response struct {
				Result  int  `json:"result"`
				Players *int `json:"player_count"`
			} `json:"response"`
		}
		if err == nil && json.Unmarshal(raw, &response) == nil && response.Response.Result == 1 {
			counts[i] = response.Response.Players
		}
	})
	next := strategyRanking{Checked: now, History: make(map[int][]strategySample, len(rankedStrategies))}
	success := false
	cutoff := now.AddDate(0, 0, -30)
	for i, source := range rankedStrategies {
		for _, sample := range old.History[source.AppID] {
			if !sample.At.Before(cutoff) && !sample.At.After(now) {
				next.History[source.AppID] = append(next.History[source.AppID], sample)
			}
		}
		if counts[i] != nil && *counts[i] >= 0 {
			success = true
			next.History[source.AppID] = append(next.History[source.AppID], strategySample{At: now, Players: *counts[i]})
		}
	}
	if !success {
		return old
	}
	next.Games = topStrategies(next.History)
	return next
}

func topStrategies(history map[int][]strategySample) []strategyRank {
	var top []strategyRank
	for _, source := range rankedStrategies {
		samples := history[source.AppID]
		if len(samples) == 0 {
			continue
		}
		total := int64(0)
		for _, sample := range samples {
			total += int64(sample.Players)
		}
		top = append(top, strategyRank{SteamSource: source, Average: int(total / int64(len(samples))), Samples: len(samples)})
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].Average != top[j].Average {
			return top[i].Average > top[j].Average
		}
		return top[i].AppID < top[j].AppID
	})
	return top[:min(len(top), 50)]
}
